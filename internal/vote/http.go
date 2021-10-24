package vote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/OpenSlides/openslides-vote-service/internal/log"
)

const (
	httpPathInternal = "/internal/vote"
	httpPathExternal = "/system/vote"
)

type creater interface {
	Create(ctx context.Context, pollID int) error
}

func handleCreate(mux *http.ServeMux, create creater) {
	mux.HandleFunc(
		httpPathInternal+"/create",
		func(w http.ResponseWriter, r *http.Request) {
			log.Debug("Receive create request: %v", r)
			if r.Method != "POST" {
				http.Error(w, MessageError{ErrInvalid, "Only POST requests are allowed"}.Error(), 405)
				return
			}

			id, err := pollID(r)
			if err != nil {
				http.Error(w, MessageError{ErrInvalid, err.Error()}.Error(), 400)
				return
			}

			if err := create.Create(r.Context(), id); err != nil {
				handleError(w, err, true)
				return
			}
		},
	)
}

// stopper stops a poll. It sets the state of the poll, so that no other user
// can vote. It writes the vote results to the writer.
type stopper interface {
	Stop(ctx context.Context, pollID int, w io.Writer) error
}

func handleStop(mux *http.ServeMux, stop stopper) {
	mux.HandleFunc(
		httpPathInternal+"/stop",
		func(w http.ResponseWriter, r *http.Request) {
			log.Debug("Receive stop request: %v", r)
			if r.Method != "POST" {
				http.Error(w, MessageError{ErrInvalid, "Only POST requests are allowed"}.Error(), 405)
				return
			}

			id, err := pollID(r)
			if err != nil {
				http.Error(w, MessageError{ErrInvalid, err.Error()}.Error(), 400)
				return
			}

			if err := stop.Stop(r.Context(), id, w); err != nil {
				handleError(w, err, true)
				return
			}
		},
	)
}

type clearer interface {
	Clear(ctx context.Context, pollID int) error
}

func handleClear(mux *http.ServeMux, clear clearer) {
	mux.HandleFunc(
		httpPathInternal+"/clear",
		func(w http.ResponseWriter, r *http.Request) {
			log.Debug("Receive clear request: %v", r)
			if r.Method != "POST" {
				http.Error(w, MessageError{ErrInvalid, "Only POST requests are allowed"}.Error(), 405)
				return
			}

			id, err := pollID(r)
			if err != nil {
				http.Error(w, MessageError{ErrInvalid, err.Error()}.Error(), 400)
				return
			}

			if err := clear.Clear(r.Context(), id); err != nil {
				handleError(w, err, true)
				return
			}
		},
	)
}

type clearAller interface {
	ClearAll(ctx context.Context) error
}

func handleClearAll(mux *http.ServeMux, clear clearAller) {
	mux.HandleFunc(
		httpPathInternal+"/clear_all",
		func(w http.ResponseWriter, r *http.Request) {
			log.Debug("Receive clear request: %v", r)
			if r.Method != "POST" {
				http.Error(w, MessageError{ErrInvalid, "Only POST requests are allowed"}.Error(), 405)
				return
			}

			if err := clear.ClearAll(r.Context()); err != nil {
				handleError(w, err, true)
				return
			}
		},
	)
}

type voter interface {
	Vote(ctx context.Context, pollID, requestUser int, r io.Reader) error
}

type authenticater interface {
	Authenticate(http.ResponseWriter, *http.Request) (context.Context, error)
	FromContext(context.Context) int
}

func handleVote(mux *http.ServeMux, vote voter, auth authenticater) {
	mux.HandleFunc(
		httpPathExternal,
		func(w http.ResponseWriter, r *http.Request) {
			log.Debug("Receive vote request: %v", r)
			if r.Method != "POST" {
				http.Error(w, MessageError{ErrInvalid, "Only POST requests are allowed"}.Error(), 405)
				return
			}

			ctx, err := auth.Authenticate(w, r)
			if err != nil {
				handleError(w, err, false)
				return
			}

			uid := auth.FromContext(ctx)
			if uid == 0 {
				http.Error(w, MessageError{ErrNotAllowed, "Anonymous user can not vote"}.Error(), 401)
				return
			}

			id, err := pollID(r)
			if err != nil {
				http.Error(w, MessageError{ErrInvalid, err.Error()}.Error(), 400)
				return
			}

			if err := vote.Vote(ctx, id, uid, r.Body); err != nil {
				handleError(w, err, false)
				return
			}
		},
	)
}

func handleHealth(mux *http.ServeMux) {
	mux.HandleFunc(
		httpPathExternal+"/health",
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"health":true}`)
		},
	)
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

func handleError(w http.ResponseWriter, err error, internal bool) {
	status := 400
	var msg string

	var errTyped interface {
		error
		Type() string
	}
	if errors.As(err, &errTyped) {
		msg = errTyped.Error()
	} else {
		// Unknown error. Handle as 500er
		status = 500
		msg = ErrInternal.Error()
		if internal {
			msg = MessageError{ErrInternal, err.Error()}.Error()
		}
		log.Info("Error: %v", err)
	}
	log.Debug("HTTP: Returning status %d", status)

	w.WriteHeader(status)
	fmt.Fprint(w, msg)
}
