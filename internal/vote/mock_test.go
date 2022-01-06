package vote_test

import (
	"context"
	"sync"
	"testing"
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
}

func (m *StubMessageBus) Publish(ctx context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, [2]string{key, string(value)})
	return nil
}

func (m *StubMessageBus) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.messages)
}
