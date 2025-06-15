package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"

	"github.com/OpenSlides/openslides-vote-service/crypto-vote/board"
	"golang.org/x/sys/unix"
)

//go:embed wrapper/crypto_vote.wasm
var crypto_vote_wasm []byte

//go:embed wrapper/crypto_vote.js
var crypto_vote_js []byte

//go:embed demoserver-static/admin.html
var admin_html []byte

//go:embed demoserver-static/client.html
var client_html []byte

//go:embed demoserver-static/htmx.min.js
var htmx_js []byte

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := InterruptContext()
	defer cancel()

	// Initialize the server
	server := NewServer()

	// Start the server
	if err := server.Run(ctx, ":8080"); err != nil {
		return fmt.Errorf("run server: %w", err)
	}

	return nil
}

type Server struct {
	mu    sync.Mutex
	board *board.Board

	pollWorkerAmount int
	secredKeys       []string
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) registerHandlers() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", s.handleStaticWithContentType(client_html, "text/html"))
	mux.Handle("/admin", s.handleStaticWithContentType(admin_html, "text/html"))
	mux.Handle("/htmx.js", s.handleStaticWithContentType(htmx_js, "application/javascript"))
	mux.Handle("/crypto_vote.wasm", s.handleStaticWithContentType(crypto_vote_wasm, "application/wasm"))
	mux.Handle("/crypto_vote.js", s.handleStaticWithContentType(crypto_vote_js, "application/javascript"))
	mux.Handle("/start", s.handleStart())
	mux.Handle("/stop", s.handleStop())
	mux.Handle("/board", s.handleBoard())
	mux.Handle("/publish_key_public", s.auth(s.handlePublishKeyPublic()))
	mux.Handle("/publish_key_secret", s.auth(s.handlePublishKeySecret()))
	mux.Handle("/vote", s.auth(s.handleVote()))

	return mux
}

func (s *Server) Run(ctx context.Context, addr string) error {
	mux := s.registerHandlers()

	srv := &http.Server{
		Addr:        addr,
		Handler:     mux,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	// Shutdown logic in separate goroutine.
	wait := make(chan error)
	go func() {
		<-ctx.Done()
		if err := srv.Shutdown(context.Background()); err != nil {
			wait <- fmt.Errorf("HTTP server shutdown: %w", err)
			return
		}
		wait <- nil
	}()

	fmt.Printf("Listen on %s\n", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP Server failed: %v", err)
	}

	return <-wait
}

func (s *Server) handleStaticWithContentType(file []byte, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write(file)
	}
}

func (s *Server) handleStart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Read JSON body
		var requestData struct {
			VoteUserIDs   []int `json:"vote_user_ids"`
			PollWorkerIDs []int `json:"poll_worker_ids"`
			VoteSize      int   `json:"vote_size"`
		}

		if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error parsing JSON: %v", err)
			return
		}

		fmt.Println(requestData)

		// Validate input
		if len(requestData.VoteUserIDs) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error: vote_user_ids cannot be empty")
			return
		}

		if len(requestData.PollWorkerIDs) < 2 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error: There must be at least two poll worker IDs")
			return
		}

		if requestData.VoteSize < 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error: vote_size must be positive")
			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		var err error
		s.board, err = boardStart(requestData.VoteUserIDs, requestData.PollWorkerIDs, requestData.VoteSize)
		if err != nil {
			w.WriteHeader(400)
			fmt.Fprintf(w, "Error: %v", err)
			return
		}

		s.pollWorkerAmount = len(requestData.PollWorkerIDs)
		s.secredKeys = nil

		if r.Header.Get("HX-Request") == "true" {
			// TODO: On HTMX request, retun some HTML
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Poll started successfully")
	}
}

func (s *Server) handleStop() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		if err := boardStop(s.board); err != nil {
			w.WriteHeader(400)
			fmt.Fprintf(w, "Error: %v", err)
			return
		}

		if r.Header.Get("HX-Request") == "true" {
			// TODO: On HTMX request, retun some HTML
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Poll stopped successfully")
	}
}

func (s *Server) handleBoard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		bb := s.board
		s.mu.Unlock()

		if bb == nil {
			w.WriteHeader(404)
			w.Write([]byte("Poll not started"))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		sseWriter := NewSSEWriter(w)

		var tid uint64
		for {
			newTID, eventList, err := bb.Receive(r.Context(), tid)
			if err != nil {
				// TODO: How to inform the client?
				return
			}
			tid = newTID

			for _, event := range eventList {
				eventJson := json.RawMessage(event)
				// TODO: Think about the event.time format
				if err := json.NewEncoder(sseWriter).Encode(eventJson); err != nil {
					// TODO: How to inform the client?
					return
				}
			}
			w.(http.Flusher).Flush()
		}
	}
}

func (s *Server) auth(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract user ID from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, "Authorization header required")
			return
		}

		// Parse "Bearer <userID>" format
		userID := 0
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			if id, err := fmt.Sscanf(authHeader[7:], "%d", &userID); err != nil || id != 1 {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "Invalid user ID in Authorization header")
				return
			}
		} else {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Invalid Authorization header format")
			return
		}

		ctx := context.WithValue(r.Context(), "userID", userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) handlePublishKeyPublic() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var publicKey string
		if err := json.NewDecoder(r.Body).Decode(&publicKey); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error: %v", err)
			return
		}

		userID := r.Context().Value("userID").(int)

		s.mu.Lock()
		defer s.mu.Unlock()

		if err := boardPublishKeyPublic(s.board, userID, publicKey); err != nil {
			w.WriteHeader(400)
			fmt.Fprintf(w, "Error: %v", err)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Public key published successfully")
	}
}

func (s *Server) handlePublishKeySecret() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var keySecred string
		if err := json.NewDecoder(r.Body).Decode(&keySecred); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error: %v", err)
			return
		}

		userID := r.Context().Value("userID").(int)
		// TODO: Check that userID is a poll worker
		_ = userID

		s.mu.Lock()
		defer s.mu.Unlock()

		s.secredKeys = append(s.secredKeys, keySecred)
		if len(s.secredKeys) != s.pollWorkerAmount {
			return
		}

		if err := boardPublishKeySecretList(s.board, s.secredKeys); err != nil {
			w.WriteHeader(400)
			fmt.Fprintf(w, "Error: %v", err)
			return
		}
	}
}

func (s *Server) handleVote() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		userID := r.Context().Value("userID").(int)

		var vote string
		if err := json.NewDecoder(r.Body).Decode(&vote); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error: %v", err)
			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		if err := boardVote(s.board, userID, vote); err != nil {
			w.WriteHeader(400)
			fmt.Fprintf(w, "Error: %v", err)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Vote submitted successfully")
	}
}

// InterruptContext works like signal.NotifyContext. It returns a context that
// is canceled, when a signal is received.
//
// It listens on os.Interrupt and unix.SIGTERM. If the signal is received two
// times, os.Exit(2) is called.
func InterruptContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, unix.SIGTERM)
		<-sig
		cancel()
		<-sig
		os.Exit(2)
	}()
	return ctx, cancel
}

type SSEWriter struct {
	writer io.Writer
}

func NewSSEWriter(w io.Writer) *SSEWriter {
	return &SSEWriter{writer: w}
}

func (s *SSEWriter) Write(p []byte) (n int, err error) {
	if _, err := s.writer.Write([]byte("data: ")); err != nil {
		return 0, err
	}

	written, err := s.writer.Write(p)
	if err != nil {
		return written, err
	}

	if _, err := s.writer.Write([]byte("\n\n")); err != nil {
		return written, err
	}

	return written, nil
}

func boardStart(voteUserIDs []int, pollWorkerIDs []int, voteSize int) (*board.Board, error) {
	data := struct {
		VoteUserIDs   []int `json:"vote_user_ids"`
		PollWorkerIDs []int `json:"poll_worker_ids"`
		VoteSize      int   `json:"vote_size"`
	}{
		VoteUserIDs:   voteUserIDs,
		PollWorkerIDs: pollWorkerIDs,
		VoteSize:      voteSize,
	}

	msg, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}

	bb, err := board.New(msg)
	if err != nil {
		return nil, fmt.Errorf("create board: %w", err)
	}
	return bb, nil
}

func boardStop(bb *board.Board) error {
	if err := bb.Add("stop", nil); err != nil {
		return fmt.Errorf("add stop message: %w", err)
	}

	return nil
}

func boardPublishKeyPublic(bb *board.Board, userID int, keyPublic string) error {
	data := struct {
		UserID    int    `json:"user_id"`
		KeyPublic string `json:"key_public"`
	}{
		UserID:    userID,
		KeyPublic: keyPublic,
	}

	msg, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	if err := bb.Add("publish_key_public", msg); err != nil {
		return fmt.Errorf("add stop message: %w", err)
	}

	return nil
}

func boardPublishKeySecretList(bb *board.Board, keySecretList []string) error {
	data := struct {
		KeySecret []string `json:"key_secret_list"`
	}{
		KeySecret: keySecretList,
	}

	msg, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	if err := bb.Add("publish_key_secret_list", msg); err != nil {
		return fmt.Errorf("add stop message: %w", err)
	}

	return nil
}

func boardVote(bb *board.Board, userID int, vote string) error {
	data := struct {
		UserID int    `json:"user_id"`
		Vote   string `json:"vote"`
	}{
		UserID: userID,
		Vote:   vote,
	}

	msg, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	if err := bb.Add("vote", msg); err != nil {
		return fmt.Errorf("add stop message: %w", err)
	}

	return nil
}
