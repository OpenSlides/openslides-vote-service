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

			pid, err := pollID(r)
			if err != nil {
				http.Error(w, MessageError{ErrInvalid, err.Error()}.Error(), 400)
				return
			}

			if err := create.Create(r.Context(), pid); err != nil {
				handleError(w, err, true)
				return
			}
		},
	)
}

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

			pid, err := pollID(r)
			if err != nil {
				http.Error(w, MessageError{ErrInvalid, err.Error()}.Error(), 400)
				return
			}

			if err := stop.Stop(r.Context(), pid, w); err != nil {
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

			pid, err := pollID(r)
			if err != nil {
				http.Error(w, MessageError{ErrInvalid, err.Error()}.Error(), 400)
				return
			}

			if err := clear.Clear(r.Context(), pid); err != nil {
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

			pid, err := pollID(r)
			if err != nil {
				http.Error(w, MessageError{ErrInvalid, err.Error()}.Error(), 400)
				return
			}

			if err := vote.Vote(ctx, pid, uid, r.Body); err != nil {
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
	rawPid := r.URL.Query().Get("pid")
	if rawPid == "" {
		return 0, fmt.Errorf("no pid argument provided")
	}

	pid, err := strconv.Atoi(rawPid)
	if err != nil {
		return 0, fmt.Errorf("pid invalid. Expected int, got %s", rawPid)
	}

	return pid, nil
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
