package board

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ostcar/topic"
)

type Board struct {
	events *topic.Topic[string]
}

func New(message json.RawMessage) (*Board, error) {
	now := time.Now() // TODO: Add a way to set the time for testing.
	event := Event{
		Time:    now,
		Type:    "start",
		Message: message,
		Hash:    "",
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("convert event: %w", err)
	}

	topic := topic.New[string]()
	topic.Publish(string(encoded))

	return &Board{
		events: topic,
	}, nil
}

func (bb *Board) Add(type_ string, message json.RawMessage) error {
	now := time.Now() // TODO: Add a way to set the time for testing.

	// TODO: maybe update topic to get an easier method to fetch the last event.
	lastEventID := bb.events.LastID()
	_, eventList, err := bb.events.Receive(context.Background(), lastEventID-1)
	if err != nil {
		return fmt.Errorf("getting last event: %w", err)
	}
	lastEvent := eventList[0]

	hash := createEventHash(lastEvent)

	event := Event{
		Time:    now,
		Type:    type_,
		Message: message,
		Hash:    hash,
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("convert event: %w", err)
	}

	bb.events.Publish(string(encoded))
	return nil
}

func (bb *Board) Receive(ctx context.Context, id uint64) (uint64, []string, error) {
	return bb.events.Receive(ctx, id)
}

type Event struct {
	Time    time.Time       `json:"time"`
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
	Hash    string          `json:"hash,omitempty"`
}

func createEventHash(event string) string {
	hash := sha256.Sum256([]byte(event))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
