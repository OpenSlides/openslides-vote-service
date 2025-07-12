// Package memory implements the vote.Backend interface.
//
// All data are saved in memory.
package memory

import (
	"context"
	"fmt"
	"maps"
	"slices"
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
	mu    sync.Mutex
	votes map[int]map[int][]byte
	state map[int]int
}

// New initializes a new memory.Backend.
func New() *Backend {
	b := Backend{
		votes: make(map[int]map[int][]byte),
		state: make(map[int]int),
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

	userIDs := slices.Collect(maps.Keys(b.votes[pollID]))
	votes := slices.Collect(maps.Values(b.votes[pollID]))
	sort.Ints(userIDs)
	return votes, userIDs, nil
}

// Vote saves a vote.
func (b *Backend) Vote(ctx context.Context, pollID int, userID int, vote []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state[pollID] == pollStateUnknown {
		return doesNotExistError{fmt.Errorf("poll is not started")}
	}

	if b.state[pollID] == pollStateStopped {
		return stoppedError{fmt.Errorf("poll is stopped")}
	}

	if b.votes[pollID] == nil {
		b.votes[pollID] = make(map[int][]byte)
	}

	if _, ok := b.votes[pollID][userID]; ok {
		return doubleVoteError{fmt.Errorf("user has already voted")}
	}

	b.votes[pollID][userID] = vote
	return nil
}

// Clear removes all data for a poll.
func (b *Backend) Clear(ctx context.Context, pollID int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.votes, pollID)
	delete(b.state, pollID)
	return nil
}

// ClearAll removes all data for all polls.
func (b *Backend) ClearAll(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.votes = make(map[int]map[int][]byte)
	b.state = make(map[int]int)
	return nil
}

// LiveVotes returns all votes from each user. Returns nil on non named votes.
func (b *Backend) LiveVotes(ctx context.Context) (map[int]map[int][]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make(map[int]map[int][]byte, len(b.votes))
	for pollID, userID2Vote := range b.votes {
		out[pollID] = maps.Clone(userID2Vote)
	}

	return out, nil
}

// AssertUserHasVoted is a method for the tests to check, if a user has voted.
func (b *Backend) AssertUserHasVoted(t *testing.T, pollID, userID int) {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.votes[pollID][userID]; !ok {
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
