package bulletin_board

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ostcar/topic"
)

type BulletinBoard struct {
	events *topic.Topic[Event]
}

func New(message json.RawMessage) BulletinBoard {
	now := time.Now() // TODO: Add a way to set the time for testing.
	event := Event{
		Time:    now,
		Message: string(message),
		Hash:    [32]byte{},
	}
	topic := topic.New[Event]()
	topic.Publish(event)

	return BulletinBoard{
		events: topic,
	}
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

	hash, err := lastEvent.createHash()
	if err != nil {
		return fmt.Errorf("create hash over last event: %w", err)
	}

	event := Event{
		Time:    now,
		Message: string(message),
		Hash:    hash,
	}
	bb.events.Publish(event)
	return nil
}

func (bb *BulletinBoard) Receive(ctx context.Context, id uint64) (uint64, []Event, error) {
	return bb.events.Receive(ctx, id)
}

type Event struct {
	Time    time.Time
	Message string // Use string to be comparable
	Hash    [32]byte
}

func (e Event) createHash() ([32]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return [32]byte{}, fmt.Errorf("convert event to json: %w", err)
	}

	return sha256.Sum256(data), nil
}
