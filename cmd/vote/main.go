package main

import (
	"context"
	"fmt"
	"io"
	golog "log"
	"os"
	"os/signal"

	"github.com/OpenSlides/openslides-vote-service/internal/log"
	"github.com/OpenSlides/openslides-vote-service/internal/vote"
)

func main() {
	ctx, cancel := interruptContext()
	defer cancel()

	if os.Getenv("OPENSLIDES_DEVELOPMENT") != "" && os.Getenv("VOTE_DISABLE_LOG") == "" {
		log.SetDebugLogger(golog.New(os.Stderr, "DEBUG ", golog.LstdFlags))
	}

	if err := vote.Run(ctx, os.Environ(), secret); err != nil {
		log.Info("Error: %v", err)
	}
}

// interruptContext works like signal.NotifyContext
//
// In only listens on os.Interrupt. If the signal is received two times,
// os.Exit(1) is called.
func interruptContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint
		cancel()

		// If the signal was send for the second time, make a hard cut.
		<-sigint
		os.Exit(1)
	}()
	return ctx, cancel
}

func secret(name string) (string, error) {
	f, err := os.Open("/run/secrets/" + name)
	if err != nil {
		return "", err
	}

	secret, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("reading `/run/secrets/%s`: %w", name, err)
	}

	return string(secret), nil
}
