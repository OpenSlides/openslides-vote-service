package vote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

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
			log.Info("Receive create request: %v", r)
			w.Header().Set("Content-Type", "application/json")

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
			log.Info("Receive stop request: %v", r)
			w.Header().Set("Content-Type", "application/json")

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
			log.Info("Receive clear request: %v", r)
			w.Header().Set("Content-Type", "application/json")

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
			log.Info("Receive clear request: %v", r)
			w.Header().Set("Content-Type", "application/json")

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
			log.Info("Receive vote request")
			w.Header().Set("Content-Type", "application/json")

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

type votedPollser interface {
	VotedPolls(ctx context.Context, pollIDs []int, requestUser int, w io.Writer) error
}

func handleVoted(mux *http.ServeMux, voted votedPollser, auth authenticater) {
	mux.HandleFunc(
		httpPathExternal+"/voted",
		func(w http.ResponseWriter, r *http.Request) {
			log.Info("Receive voted request: %v", r)
			w.Header().Set("Content-Type", "application/json")

			if r.Method != "GET" {
				http.Error(w, MessageError{ErrInvalid, "Only GET requests are allowed"}.Error(), 405)
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

			pollIDs, err := pollsID(r)
			if err != nil {
				http.Error(w, MessageError{ErrInvalid, err.Error()}.Error(), 400)
				return
			}

			if err := voted.VotedPolls(ctx, pollIDs, uid, w); err != nil {
				handleError(w, err, false)
				return
			}
		},
	)
}

type voteCounter interface {
	VoteCount(ctx context.Context, pollIDs []int, w io.Writer) error
}

func handleVoteCount(mux *http.ServeMux, voteCounter voteCounter) {
	mux.HandleFunc(
		httpPathInternal+"/vote_count",
		func(w http.ResponseWriter, r *http.Request) {
			log.Info("Receive vote count request: %v", r)
			w.Header().Set("Content-Type", "application/json")

			pollIDs, err := parseVoteCountRequest(r.Body)
			if err != nil {
				handleError(w, fmt.Errorf("parse request: %w", err), true)
				return
			}

			if err := voteCounter.VoteCount(r.Context(), pollIDs, w); err != nil {
				handleError(w, err, true)
				return
			}

		},
	)
}

func handleHealth(mux *http.ServeMux) {
	mux.HandleFunc(
		httpPathExternal+"/health",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

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

func pollsID(r *http.Request) ([]int, error) {
	rawIDs := strings.Split(r.URL.Query().Get("ids"), ",")

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

func parseVoteCountRequest(r io.Reader) ([]int, error) {
	var data struct {
		Keys []string `json:"requests"`
	}
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return nil, fmt.Errorf("decoding request body from jsin: %w", err)
	}

	var ids []int
	for i, key := range data.Keys {
		keyParts := strings.SplitN(key, "/", 3)

		if keyParts[0] != "poll" || keyParts[2] != "vote_count" {
			continue
		}

		id, err := strconv.Atoi(keyParts[1])
		if err != nil {
			return nil, fmt.Errorf("parsing %dth key %s is not an int", i, keyParts[1])
		}
		ids = append(ids, id)
	}

	return ids, nil
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
