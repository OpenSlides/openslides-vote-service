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
	"strings"
	"time"

	"github.com/OpenSlides/openslides-go/environment"
	"github.com/OpenSlides/openslides-vote-service/log"
	"github.com/OpenSlides/openslides-vote-service/vote"
)

var envVotePort = environment.NewVariable("VOTE_PORT", "9013", "Port on which the service listen on.")

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
	ticketProvider := func() (<-chan time.Time, func()) {
		ticker := time.NewTicker(time.Second)
		return ticker.C, ticker.Stop
	}

	mux := registerHandlers(service, auth, ticketProvider)

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

	log.Info("Listen on %s\n", s.Addr)
	if err := srv.Serve(s.lst); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP Server failed: %v", err)
	}

	return <-wait
}

type voteService interface {
	starter
	stopper
	clearer
	clearAller
	voteCounter
	voter
	haveIvoteder
}

type authenticater interface {
	Authenticate(http.ResponseWriter, *http.Request) (context.Context, error)
	FromContext(context.Context) int
}

func registerHandlers(service voteService, auth authenticater, ticketProvider func() (<-chan time.Time, func())) *http.ServeMux {
	const (
		internal = "/internal/vote"
		external = "/system/vote"
	)

	mux := http.NewServeMux()

	mux.Handle(internal+"/start", handleInternal(handleStart(service)))
	mux.Handle(internal+"/stop", handleInternal(handleStop(service)))
	mux.Handle(internal+"/clear", handleInternal(handleClear(service)))
	mux.Handle(internal+"/clear_all", handleInternal(handleClearAll(service)))
	mux.Handle(internal+"/vote_count", handleInternal(handleVoteCount(service, ticketProvider)))
	mux.Handle(external+"", handleExternal(handleVote(service, auth)))
	mux.Handle(external+"/voted", handleExternal(handleVoted(service, auth)))
	mux.Handle(external+"/health", handleExternal(handleHealth()))

	return mux
}

type starter interface {
	Start(ctx context.Context, pollID int) error
}

func handleStart(start starter) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		log.Info("Receiving start request")
		w.Header().Set("Content-Type", "application/json")

		id, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		return start.Start(r.Context(), id)
	}
}

// stopper stops a poll. It sets the state of the poll, so that no other user
// can vote. It writes the vote results to the writer.
type stopper interface {
	Stop(ctx context.Context, pollID int) (vote.StopResult, error)
}

func handleStop(stop stopper) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		log.Info("Receiving stop request")
		w.Header().Set("Content-Type", "application/json")

		id, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		result, err := stop.Stop(r.Context(), id)
		if err != nil {
			return err
		}

		// Convert vote objects to json.RawMessage
		encodableObjects := make([]json.RawMessage, len(result.Votes))
		for i := range result.Votes {
			encodableObjects[i] = result.Votes[i]
		}

		if result.UserIDs == nil {
			result.UserIDs = []int{}
		}

		out := struct {
			Votes []json.RawMessage `json:"votes"`
			Users []int             `json:"user_ids"`
		}{
			encodableObjects,
			result.UserIDs,
		}

		if err := json.NewEncoder(w).Encode(out); err != nil {
			return fmt.Errorf("encoding and sending objects: %w", err)
		}
		return nil
	}
}

type clearer interface {
	Clear(ctx context.Context, pollID int) error
}

func handleClear(clear clearer) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		log.Info("Receiving clear request")
		w.Header().Set("Content-Type", "application/json")

		id, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		return clear.Clear(r.Context(), id)
	}
}

type clearAller interface {
	ClearAll(ctx context.Context) error
}

func handleClearAll(clear clearAller) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		log.Info("Receiving clear all request")
		w.Header().Set("Content-Type", "application/json")

		return clear.ClearAll(r.Context())
	}
}

type voter interface {
	Vote(ctx context.Context, pollID, requestUser int, r io.Reader) error
}

func handleVote(service voter, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		log.Info("Receiving vote request")
		w.Header().Set("Content-Type", "application/json")

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

type haveIvoteder interface {
	Voted(ctx context.Context, pollIDs []int, requestUser int) (map[int][]int, error)
}

func handleVoted(voted haveIvoteder, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		log.Info("Receiving has voted request")
		w.Header().Set("Content-Type", "application/json")

		ctx, err := auth.Authenticate(w, r)
		if err != nil {
			return err
		}

		uid := auth.FromContext(ctx)
		if uid == 0 {
			return statusCode(401, vote.MessageError(vote.ErrNotAllowed, "Anonymous user can not vote"))
		}

		pollIDs, err := pollsID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		voted, err := voted.Voted(ctx, pollIDs, uid)
		if err != nil {
			return err
		}

		if err := json.NewEncoder(w).Encode(voted); err != nil {
			return fmt.Errorf("encoding and sending objects: %w", err)
		}

		return nil
	}
}

type voteCounter interface {
	VoteCount(ctx context.Context) map[int]int
}

func handleVoteCount(voteCounter voteCounter, eventer func() (<-chan time.Time, func())) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		log.Info("Receiving vote count request")
		w.Header().Set("Content-Type", "application/json")

		encoder := json.NewEncoder(w)

		event, cancel := eventer()
		defer cancel()

		var countMemory map[int]int
		firstData := true
		for {
			count := voteCounter.VoteCount(r.Context())

			if countMemory == nil {
				countMemory = count
			} else {
				for k := range countMemory {
					if _, ok := count[k]; !ok {
						count[k] = 0
					}
					if count[k] == countMemory[k] {
						delete(count, k)
						continue
					}
					countMemory[k] = count[k]
				}

				for k := range count {
					if _, ok := countMemory[k]; !ok {
						countMemory[k] = count[k]
					}
				}
			}

			if firstData || len(count) > 0 {
				firstData = false
				if err := encoder.Encode(count); err != nil {
					return err
				}
			}

			// This could be in the if(count) block, but the Flush is used
			// in the tests and has to be called, even when there is no data
			// to sent.
			w.(http.Flusher).Flush()

			select {
			case _, ok := <-event:
				if !ok {
					return nil
				}
			case <-r.Context().Done():
				return nil
			}
		}
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

func pollsID(r *http.Request) ([]int, error) {
	rawIDs := strings.Split(r.URL.Query().Get("ids"), ",")
	if len(rawIDs) == 0 {
		return nil, fmt.Errorf("no ids argument provided")
	}

	ids := make([]int, len(rawIDs))
	for i, rawID := range rawIDs {
		id, err := strconv.Atoi(rawID)
		if err != nil {
			return nil, fmt.Errorf("%dth id invalid. Expected int, got %s", i, rawID)
		}
		ids[i] = id
	}

	return ids, nil
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
