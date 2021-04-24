package vote_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/OpenSlides/openslides-vote-service/internal/vote"
)

func TestRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := testLog{}

	t.Run("Start Server with default port", func(t *testing.T) {
		var runErr error
		go func() {
			runErr = vote.Run(ctx, []string{"VOTE_BACKEND_FAST=memory", "VOTE_BACKEND_LONG=memory"}, secret, log.Printf)
		}()

		_, err := net.DialTimeout("tcp", "localhost:9013", 10*time.Millisecond)
		if err != nil {
			t.Fatalf("Server could not be reached: %v", err)
		}

		if runErr != nil {
			t.Errorf("Vote.Run retunred unexpected error: %v", err)
		}

		if got := log.LastMSG(); got != "Listen on :9013" {
			t.Errorf("Expected listen on message, got: %s", got)
		}
	})

	t.Run("Start Server with given port", func(t *testing.T) {
		var err error
		go func() {
			err = vote.Run(ctx, []string{"VOTE_BACKEND_FAST=memory", "VOTE_BACKEND_LONG=memory", "VOTE_PORT=5000"}, secret, log.Printf)
		}()

		if _, err := net.DialTimeout("tcp", "localhost:5000", 10*time.Millisecond); err != nil {
			t.Errorf("Server could not be reached: %v", err)
		}

		if err != nil {
			t.Errorf("Vote.Run retunred unexpected error: %v", err)
		}

		if got := log.LastMSG(); got != "Listen on :5000" {
			t.Errorf("Expected listen on message, got: %s", got)
		}
	})

	t.Run("Cancel Server", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		var runErr error
		done := make(chan struct{})
		go func() {
			// Use an individuel port because the default port could be used by other tests.
			runErr = vote.Run(ctx, []string{"VOTE_BACKEND_FAST=memory", "VOTE_BACKEND_LONG=memory", "VOTE_PORT=5001"}, secret, log.Printf)
			close(done)
		}()

		// Wait for the server to start.
		conn, err := net.DialTimeout("tcp", "localhost:5001", 10*time.Millisecond)
		if err != nil {
			t.Fatalf("Server could not be reached: %v", err)
		}
		conn.Close()

		// Stop the context.
		cancel()

		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			t.Errorf("Server did not stop")
		}

		if runErr != nil {
			t.Errorf("Vote.Run retunred unexpected error: %v", runErr)
		}
	})

	t.Run("Registered handlers", func(t *testing.T) {
		var runErr error
		go func() {
			// Use an individuel port because the default port could be used by other tests.
			runErr = vote.Run(ctx, []string{"VOTE_BACKEND_FAST=memory", "VOTE_BACKEND_LONG=memory", "VOTE_PORT=5002"}, secret, log.Printf)
		}()

		// Wait for the server to start.
		conn, err := net.DialTimeout("tcp", "localhost:5002", 10*time.Millisecond)
		if err != nil {
			t.Fatalf("Server could not be reached: %v", err)
		}
		conn.Close()

		baseUrl := "http://localhost:5002"

		for _, path := range []string{
			"/internal/vote/create",
			"/internal/vote/stop",
			"/internal/vote/clear",
			"/system/vote",
			"/system/vote/health",
		} {
			t.Run(path, func(t *testing.T) {
				resp, err := http.Get(baseUrl + path)
				if err != nil {
					t.Fatalf("Can not open connection: %v", err)
				}

				if resp.StatusCode == 404 {
					t.Errorf("Got status %s", resp.Status)
				}
			})
		}

		if runErr != nil {
			t.Errorf("Vote.Run retunred unexpected error: %v", runErr)
		}
	})
}

type testLog struct {
	mu      sync.Mutex
	lastMSG string
}

func (l *testLog) Printf(format string, a ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.lastMSG = fmt.Sprintf(format, a...)
}

func (l *testLog) LastMSG() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.lastMSG
}

func secret(name string) (string, error) {
	return "secret", nil
}
