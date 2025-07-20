package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/OpenSlides/openslides-vote-service/vote"
)

func resolveError(handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := handler.ServeHTTP(w, r)
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}

		writeStatusCode(w, err)
		writeFormattedError(w, err)
	}
}

func writeStatusCode(w http.ResponseWriter, err error) {
	statusCode := 400
	var errStatusCode statusCodeError
	if errors.As(err, &errStatusCode) {
		statusCode = errStatusCode.code
	}

	var errTyped interface {
		Type() string
	}
	if !errors.As(err, &errTyped) || errTyped.Type() == "internal" {
		statusCode = 500
	}

	w.WriteHeader(statusCode)
}

func writeFormattedError(w io.Writer, err error) {
	errType := "internal"
	var errTyped interface {
		error
		Type() string
	}
	if errors.As(err, &errTyped) {
		errType = errTyped.Type()
	}

	msg := err.Error()
	if errType == "internal" {
		fmt.Printf("Error: %s\n", msg)
		msg = vote.ErrInternal.Error()
	}

	out := struct {
		Error string `json:"error"`
		MSG   string `json:"message"`
	}{
		errType,
		msg,
	}

	if err := json.NewEncoder(w).Encode(out); err != nil {
		fmt.Printf("Error encoding error message: %v\n", err)
		fmt.Fprint(w, `{"error":"internal", "message":"Something went wrong encoding the error message"}`)
	}
}

type statusCodeError struct {
	err  error
	code int
}

func (s statusCodeError) Error() string {
	return fmt.Sprintf("%d - %v", s.code, s.err)
}

func (s statusCodeError) Unwrap() error {
	return s.err
}

func statusCode(code int, err error) error {
	return statusCodeError{err, code}
}
