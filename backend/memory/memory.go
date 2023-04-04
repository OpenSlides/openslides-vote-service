// Package memory implements the vote.Backend interface.
//
// All data are saved in memory.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
)

const (
	pollStateUnknown = iota
	pollStateStarted
	pollStateStopped
)

// Backend is a vote backend that holds the data in memory.
type Backend struct {
	mu      sync.Mutex
	voted   map[int]map[int]struct{}
	objects map[int][][]byte
	state   map[int]int
}

// New initializes a new memory.Backend.
func New() *Backend {
	b := Backend{
		voted:   make(map[int]map[int]struct{}),
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

	if b.state[pollID] == pollStateStopped {
		return nil
	}
	b.state[pollID] = pollStateStarted
	return nil
}

// Stop stopps a poll.
func (b *Backend) Stop(ctx context.Context, pollID int) ([][]byte, []int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state[pollID] == pollStateUnknown {
		return nil, nil, doesNotExistError{fmt.Errorf("Poll does not exist")}
	}

	b.state[pollID] = pollStateStopped

	userIDs := make([]int, 0, len(b.voted[pollID]))
	for id := range b.voted[pollID] {
		userIDs = append(userIDs, id)
	}
	sort.Ints(userIDs)
	return b.objects[pollID], userIDs, nil
}

// Vote saves a vote.
func (b *Backend) Vote(ctx context.Context, pollID int, userID int, object []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state[pollID] == pollStateUnknown {
		return doesNotExistError{fmt.Errorf("poll is not started")}
	}

	if b.state[pollID] == pollStateStopped {
		return stoppedError{fmt.Errorf("poll is stopped")}
	}

	if b.voted[pollID] == nil {
		b.voted[pollID] = make(map[int]struct{})
	}

	if _, ok := b.voted[pollID][userID]; ok {
		return doubleVoteError{fmt.Errorf("user has already voted")}
	}

	b.voted[pollID][userID] = struct{}{}
	b.objects[pollID] = append(b.objects[pollID], object)
	return nil
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

// ClearAll removes all data for all polls.
func (b *Backend) ClearAll(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.voted = make(map[int]map[int]struct{})
	b.objects = make(map[int][][]byte)
	b.state = make(map[int]int)
	return nil
}

// Voted returns for all polls, which users have voted.
func (b *Backend) Voted(ctx context.Context) (map[int][]int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make(map[int][]int, len(b.voted))
	for pid, userIDs := range b.voted {
		out[pid] = make([]int, 0, len(userIDs))
		for userID := range userIDs {
			out[pid] = append(out[pid], userID)
		}

		sort.Ints(out[pid])
	}

	return out, nil
}

// AssertUserHasVoted is a method for the tests to check, if a user has voted.
func (b *Backend) AssertUserHasVoted(t *testing.T, pollID, userID int) {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.voted[pollID][userID]; !ok {
		t.Errorf("User %d has not voted", userID)
	}
}

type doesNotExistError struct {
	error
}

func (doesNotExistError) DoesNotExist() {}

type doubleVoteError struct {
	error
}

func (doubleVoteError) DoubleVote() {}

type stoppedError struct {
	error
}

func (stoppedError) Stopped() {}
