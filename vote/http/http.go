package http

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/OpenSlides/openslides-go/environment"
	"github.com/OpenSlides/openslides-vote-service/vote"
)

var envVotePort = environment.NewVariable("VOTE_PORT", "9013", "Port on which the service listens on.")

// Server can start the service on a port.
type Server struct {
	Addr string
	lst  net.Listener
}

// New initializes a new Server.
func New(lookup environment.Environmenter) Server {
	return Server{
		Addr: ":" + envVotePort.Value(lookup),
	}
}

// StartListener starts the listener where the server will listen on.
//
// This is usefull for testing so an empty port will be dissolved.
func (s *Server) StartListener() error {
	lst, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("open %s: %w", s.Addr, err)
	}

	s.lst = lst
	s.Addr = lst.Addr().String()
	return nil
}

// Run starts the http service.
func (s *Server) Run(ctx context.Context, auth authenticater, service *vote.Vote) error {
	mux := registerHandlers(service, auth)

	srv := &http.Server{
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

	if s.lst == nil {
		if err := s.StartListener(); err != nil {
			return fmt.Errorf("start listening: %w", err)
		}
	}

	fmt.Printf("Listen on %s\n", s.Addr)
	if err := srv.Serve(s.lst); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP Server failed: %w", err)
	}

	return <-wait
}

type voteService interface {
	starter
	finalizer
	voter
}

type authenticater interface {
	Authenticate(http.ResponseWriter, *http.Request) (context.Context, error)
	FromContext(context.Context) int
}

func registerHandlers(service voteService, auth authenticater) *http.ServeMux {
	const base = "/system/vote"

	mux := http.NewServeMux()

	mux.Handle(base, resolveError(handleVote(service, auth)))
	mux.Handle(base+"/start", resolveError(handleStart(service, auth)))
	mux.Handle(base+"/finalize", resolveError(handleFinalize(service, auth)))
	mux.Handle(base+"/health", resolveError(handleHealth()))

	return mux
}

type starter interface {
	Start(ctx context.Context, pollID int, requestUserID int) error
}

func handleStart(start starter, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			return vote.MessageError(vote.ErrInvalid, "Only POST method is allowed")
		}

		ctx, err := auth.Authenticate(w, r)
		if err != nil {
			return err
		}

		uid := auth.FromContext(ctx)
		if uid == 0 {
			return statusCode(401, vote.MessageError(vote.ErrNotAllowed, "Anonymous user can not start a poll"))
		}

		id, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		return start.Start(r.Context(), id, uid)
	}
}

type finalizer interface {
	Finalize(ctx context.Context, pollID int, requestUserID int, publish bool, anonymize bool) error
}

func handleFinalize(finalize finalizer, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			return vote.MessageError(vote.ErrInvalid, "Only POST method is allowed")
		}

		ctx, err := auth.Authenticate(w, r)
		if err != nil {
			return err
		}

		uid := auth.FromContext(ctx)
		if uid == 0 {
			return statusCode(401, vote.MessageError(vote.ErrNotAllowed, "Anonymous user can not stop a poll"))
		}

		id, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		publish := r.URL.Query().Has("publish")
		anonymize := r.URL.Query().Has("anonymize")

		return finalize.Finalize(r.Context(), id, uid, publish, anonymize)
	}
}

type voter interface {
	Vote(ctx context.Context, pollID, requestUser int, r io.Reader) error
}

func handleVote(service voter, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			return vote.MessageError(vote.ErrInvalid, "Only POST method is allowed")
		}

		ctx, err := auth.Authenticate(w, r)
		if err != nil {
			return err
		}

		uid := auth.FromContext(ctx)
		if uid == 0 {
			return statusCode(401, vote.MessageError(vote.ErrNotAllowed, "Anonymous user can not vote"))
		}

		id, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		return service.Vote(ctx, id, uid, r.Body)
	}
}

func handleHealth() HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "application/json")

		fmt.Fprintf(w, `{"healthy":true}`)
		return nil
	}
}

// HealthClient sends a http request to a server to fetch the health status.
func HealthClient(ctx context.Context, useHTTPS bool, host, port string, insecure bool) error {
	proto := "http"
	if useHTTPS {
		proto = "https"
	}

	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		fmt.Sprintf("%s://%s:%s/system/vote/health", proto, host, port),
		nil,
	)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if insecure {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("health returned status %s", resp.Status)
	}

	var body struct {
		Healthy bool `json:"healthy"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("reading and parsing response body: %w", err)
	}

	if !body.Healthy {
		return fmt.Errorf("Server returned unhealthy response")
	}

	return nil
}

func pollID(r *http.Request) (int, error) {
	rawID := r.URL.Query().Get("id")
	if rawID == "" {
		return 0, fmt.Errorf("no id argument provided")
	}

	id, err := strconv.Atoi(rawID)
	if err != nil {
		return 0, fmt.Errorf("id invalid. Expected int, got %s", rawID)
	}

	return id, nil
}

// Handler is like http.Handler but returns an error
type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request) error
}

// HandlerFunc is like http.HandlerFunc but returns an error
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

func (f HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	return f(w, r)
}
