package vote_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type StubGetter struct {
	data      map[string][]byte
	err       error
	requested map[string]bool
}

func (g *StubGetter) Get(ctx context.Context, keys ...string) (map[string][]byte, error) {
	if g.err != nil {
		return nil, g.err
	}
	if g.requested == nil {
		g.requested = make(map[string]bool)
	}

	out := make(map[string][]byte, len(keys))
	for _, k := range keys {
		out[k] = g.data[k]
		g.requested[k] = true
	}
	return out, nil
}

func (g *StubGetter) assertKeys(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if !g.requested[key] {
			t.Errorf("Key %s is was not requested", key)
		}
	}
}

type StubMessageBus struct {
	mu       sync.Mutex
	messages [][2]string
	ch       chan [2]string
}

func NewStubMessageBus() *StubMessageBus {
	return &StubMessageBus{
		ch: make(chan [2]string, 100),
	}
}

func (m *StubMessageBus) Publish(ctx context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	msg := [2]string{key, string(value)}

	m.messages = append(m.messages, msg)
	m.ch <- msg
	return nil
}

func (m *StubMessageBus) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.messages)
}

// Read reads the next message from the bus. Blogs until the message is ready.
//
// Only one subscriber is supported.
func (m *StubMessageBus) Read(timeout time.Duration) ([2]string, error) {
	if timeout == 0 {
		return <-m.ch, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case msg := <-m.ch:
		return msg, nil
	case <-timer.C:
		return [2]string{}, errors.New("timeout")
	}
}

type MockCounter struct {
	mu sync.Mutex

	counts  map[int]int // map from pollID to number of votes
	id      uint64
	changes map[uint64]int // map from id to pollID

	wait chan struct{}
}

func NewMockCounter() *MockCounter {
	return &MockCounter{
		counts:  make(map[int]int),
		changes: make(map[uint64]int),
		wait:    make(chan struct{}),
	}
}

// Add adds one vote for the pollID to the counter.
func (c *MockCounter) CountAdd(ctx context.Context, pollID int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.counts[pollID]++
	c.id++
	c.changes[c.id] = pollID
	c.wakeup()

	return nil
}

// Clear deletes all counts for a poll.
func (c *MockCounter) CountClear(ctx context.Context, pollID int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.counts, pollID)
	c.id++
	c.changes[c.id] = pollID
	c.wakeup()
	return nil
}

// ClearAll deleted all counts from all polls.
func (c *MockCounter) ClearAll(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.counts = make(map[int]int)
	c.id = 0
	c.changes = make(map[uint64]int)
	c.wakeup()
	return nil
}

// Counters returns all counts of all polls since the given id.
//
// Returns a new ID that can be used the next time. Returns all counts for
// all polls if the id 0 is given.
//
// Blocks until there is new data.
func (c *MockCounter) Counters(ctx context.Context, id uint64) (newid uint64, counts map[int]int, err error) {
	c.mu.Lock()

	if id == 0 {
		defer c.mu.Unlock()
		return c.id, c.counts, nil
	}

	if id < c.id {
		defer c.mu.Unlock()
		votes := make(map[int]int)
		for cid, pollID := range c.changes {
			if cid <= id {
				continue
			}
			votes[pollID] = c.counts[pollID]
		}
		return c.id, votes, err
	}

	ch := c.wait

	c.mu.Unlock()
	select {
	case <-ch:
		// Try again. This should not be used in production. If id is a very
		// high number, this will run in a stack overflow.
		return c.Counters(ctx, id)

	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

// WaitX waits for x votes before returing.
func (c *MockCounter) WaitForID(id uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		ch := c.wait

		c.mu.Unlock()
		<-ch
		c.mu.Lock()

		if id >= c.id {
			break
		}
	}
}

func (c *MockCounter) wakeup() {
	close(c.wait)
	c.wait = make(chan struct{})
}
