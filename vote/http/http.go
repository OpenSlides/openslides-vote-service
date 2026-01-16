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
	Addr   string
	lst    net.Listener
	logger logger
}

// New initializes a new Server.
func New(lookup environment.Environmenter, logger logger) Server {
	return Server{
		Addr:   ":" + envVotePort.Value(lookup),
		logger: logger,
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
	mux := registerHandlers(service, auth, s.logger)

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

	s.logger("Listen on %s\n", s.Addr)
	if err := srv.Serve(s.lst); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP Server failed: %w", err)
	}

	return <-wait
}

type voteService interface {
	creater
	updater
	deleter
	starter
	finalizer
	reseter
	voter
}

type authenticater interface {
	Authenticate(http.ResponseWriter, *http.Request) (context.Context, error)
	FromContext(context.Context) int
}

func registerHandlers(service voteService, auth authenticater, logger logger) *http.ServeMux {
	const base = "/system/vote"

	resolveError := getResolveError(logger)

	mux := http.NewServeMux()

	mux.Handle(base+"/create", resolveError(handleCreate(service, auth)))
	mux.Handle(base+"/update", resolveError(handleUpdate(service, auth)))
	mux.Handle(base+"/delete", resolveError(handleDelete(service, auth)))
	mux.Handle(base+"/start", resolveError(handleStart(service, auth)))
	mux.Handle(base+"/finalize", resolveError(handleFinalize(service, auth)))
	mux.Handle(base+"/reset", resolveError(handleReset(service, auth)))
	mux.Handle(base, resolveError(handleVote(service, auth)))
	mux.Handle(base+"/health", resolveError(handleHealth()))

	return mux
}

type creater interface {
	Create(ctx context.Context, requestUserID int, r io.Reader) (int, error)
}

func handleCreate(create creater, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx, uid, err := prepareRequest(w, r, auth)
		if err != nil {
			return fmt.Errorf("prepare request: %w", err)
		}

		pollID, err := create.Create(ctx, uid, r.Body)
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}

		result := struct {
			PollID int `json:"poll_id"`
		}{pollID}

		if err := json.NewEncoder(w).Encode(result); err != nil {
			return fmt.Errorf("encoding and sending poll id: %w", err)
		}

		return nil
	}
}

type updater interface {
	Update(ctx context.Context, pollID int, requestUserID int, r io.Reader) error
}

func handleUpdate(update updater, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx, uid, err := prepareRequest(w, r, auth)
		if err != nil {
			return fmt.Errorf("prepare request: %w", err)
		}

		pollID, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		if err := update.Update(ctx, pollID, uid, r.Body); err != nil {
			return fmt.Errorf("update: %w", err)
		}

		return nil
	}
}

type deleter interface {
	Delete(ctx context.Context, pollID int, requestUserID int) error
}

func handleDelete(delete deleter, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx, uid, err := prepareRequest(w, r, auth)
		if err != nil {
			return fmt.Errorf("prepare request: %w", err)
		}

		pollID, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		if err := delete.Delete(ctx, pollID, uid); err != nil {
			return fmt.Errorf("delete: %w", err)
		}

		return nil
	}
}

type starter interface {
	Start(ctx context.Context, pollID int, requestUserID int) error
}

func handleStart(start starter, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx, uid, err := prepareRequest(w, r, auth)
		if err != nil {
			return fmt.Errorf("prepare request: %w", err)
		}

		id, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		return start.Start(ctx, id, uid)
	}
}

type finalizer interface {
	Finalize(ctx context.Context, pollID int, requestUserID int, publish bool, anonymize bool) error
}

func handleFinalize(finalize finalizer, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx, uid, err := prepareRequest(w, r, auth)
		if err != nil {
			return fmt.Errorf("prepare request: %w", err)
		}

		id, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		publish := r.URL.Query().Has("publish")
		anonymize := r.URL.Query().Has("anonymize")

		return finalize.Finalize(ctx, id, uid, publish, anonymize)
	}
}

type reseter interface {
	Reset(ctx context.Context, pollID int, requestUserID int) error
}

func handleReset(reset reseter, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx, uid, err := prepareRequest(w, r, auth)
		if err != nil {
			return fmt.Errorf("prepare request: %w", err)
		}

		pollID, err := pollID(r)
		if err != nil {
			return vote.WrapError(vote.ErrInvalid, err)
		}

		if err := reset.Reset(ctx, pollID, uid); err != nil {
			return fmt.Errorf("delete: %w", err)
		}

		return nil
	}
}

type voter interface {
	Vote(ctx context.Context, pollID, requestUser int, r io.Reader) error
}

func handleVote(service voter, auth authenticater) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx, uid, err := prepareRequest(w, r, auth)
		if err != nil {
			return fmt.Errorf("prepare request: %w", err)
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

		fmt.Fprintf(w, `{"healthy": true, "service":"vote"}`)
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
		Healthy bool   `json:"healthy"`
		Service string `json:"service"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("reading and parsing response body: %w", err)
	}

	if !body.Healthy || body.Service != "vote" {
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

// prepare Requests bundles the functionality needed for all handlers.
//
// - sets the header Content-Type to application/json
// - authenticates the user
// - returns the authenticated ctx and the request user id
func prepareRequest(w http.ResponseWriter, r *http.Request, auth authenticater) (context.Context, int, error) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		return nil, 0, vote.MessageError(vote.ErrInvalid, "Only POST method is allowed")
	}

	ctx, err := auth.Authenticate(w, r)
	if err != nil {
		return nil, 0, fmt.Errorf("authenticate request user: %w", err)
	}

	uid := auth.FromContext(ctx)
	if uid == 0 {
		return nil, 0, statusCode(401, vote.MessageError(vote.ErrNotAllowed, "Anonymous user can not use the vote service"))
	}

	return ctx, uid, nil
}
