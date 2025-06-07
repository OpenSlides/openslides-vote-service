package bulletin_board

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ostcar/topic"
)

type BulletinBoard struct {
	events *topic.Topic[string]
}

func New(message json.RawMessage) (BulletinBoard, error) {
	now := time.Now() // TODO: Add a way to set the time for testing.
	event := Event{
		Time:    now,
		Message: message,
		Hash:    "",
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		return BulletinBoard{}, fmt.Errorf("convert event: %w", err)
	}

	topic := topic.New[string]()
	topic.Publish(string(encoded))

	return BulletinBoard{
		events: topic,
	}, nil
}

func (bb *BulletinBoard) Add(message json.RawMessage) error {
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

func (bb *BulletinBoard) Receive(ctx context.Context, id uint64) (uint64, []string, error) {
	return bb.events.Receive(ctx, id)
}

type Event struct {
	Time    time.Time
	Message json.RawMessage
	Hash    string
}

func createEventHash(event string) string {
	hash := sha256.Sum256([]byte(event))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
