// Package memory implements the vote.Backend interface.
//
// All data are saved in memory. The main use is testing.
package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// Backend is a simple (not concurent) vote backend that can be used for
// testing.
type Backend struct {
	mu      sync.Mutex
	voted   map[int]map[int]bool
	objects map[int][][]byte
	state   map[int]int
}

// New initializes a new memory.Backend.
func New() *Backend {
	b := Backend{
		voted:   make(map[int]map[int]bool),
		objects: make(map[int][][]byte),
		state:   make(map[int]int),
	}
	return &b
}

func (b *Backend) String() string {
	return "memory"
}

// Start opens opens a poll.
func (b *Backend) Start(ctx context.Context, pollID int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state[pollID] == 2 {
		return nil
	}
	b.state[pollID] = 1
	return nil
}

// Vote saves a vote.
func (b *Backend) Vote(ctx context.Context, pollID int, userID int, object []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state[pollID] == 0 {
		return doesNotExistError{fmt.Errorf("poll is not open")}
	}

	if b.state[pollID] == 2 {
		return stoppedError{fmt.Errorf("Poll is stopped")}
	}

	if b.voted[pollID] == nil {
		b.voted[pollID] = make(map[int]bool)
	}

	if _, ok := b.voted[pollID][userID]; ok {
		return doupleVoteError{fmt.Errorf("user has already voted")}
	}

	b.voted[pollID][userID] = true
	b.objects[pollID] = append(b.objects[pollID], object)
	return nil
}

// Stop stopps a poll
func (b *Backend) Stop(ctx context.Context, pollID int) ([][]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state[pollID] == 0 {
		return nil, doesNotExistError{fmt.Errorf("Poll does not exist")}
	}

	b.state[pollID] = 2
	return b.objects[pollID], nil
}

// Clear removes all data for a poll.
func (b *Backend) Clear(ctx context.Context, pollID int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.voted, pollID)
	delete(b.objects, pollID)
	delete(b.state, pollID)
	return nil
}

//AssertUserHasVoted is a method for the tests to check, if a user has voted.
func (b *Backend) AssertUserHasVoted(t *testing.T, pollID, userID int) {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.voted[pollID][userID] {
		t.Errorf("User %d has not voted", userID)
	}
}

type doesNotExistError struct {
	error
}

func (doesNotExistError) DoesNotExist() {}

type doupleVoteError struct {
	error
}

func (doupleVoteError) DoupleVote() {}

type stoppedError struct {
	error
}

func (stoppedError) Stopped() {}
