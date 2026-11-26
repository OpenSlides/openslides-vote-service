package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/OpenSlides/openslides-vote-service/vote"
)

type logger func(fmt string, a ...any) (int, error)

func getResolveError(logger logger) func(handler Handler) http.HandlerFunc {
	return func(handler Handler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			err := handler.ServeHTTP(w, r)
			if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}

			writeStatusCode(w, err)
			writeFormattedError(w, err, logger)
		}
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

func writeFormattedError(w io.Writer, err error, logger logger) {
	errType := "internal"
	msg := err.Error()
	var errTyped interface {
		error
		Type() string
	}
	if errors.As(err, &errTyped) {
		errType = errTyped.Type()
		msg = errTyped.Error()
	}

	if errType == "internal" {
		logger("Error: %s\n", msg)
		msg = vote.ErrInternal.Error()
	}

	w.Write([]byte(errorAsJSON(errType, msg)))
}

func errorAsJSON(errType string, msg string) string {
	return fmt.Sprintf(`{"error":"%s","message":"%s"}`, errType, msg)
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
