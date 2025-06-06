package bulletin_board_test

import (
	"context"
	"testing"
	"time"

	"github.com/OpenSlides/openslides-vote-service/crypto-vote/bulletin_board"
)

func TestBulletinBoard(t *testing.T) {
	board := bulletin_board.New([]byte(`{"message": "hello world"`))
	board.Add(42, []byte(`{"message": "next message"`))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	_, expected, err := board.Receive(ctx, 0)
	if err != nil {
		t.Fatalf("board.Receive: %v", err)
	}

	if len(expected) != 2 {
		t.Fatalf("Got %d elements in board, expected 2", len(expected))
	}

	if expected[0].Hash != [32]byte{} {
		t.Errorf("Expected empty hash on first message")
	}

	if expected[1].Hash == [32]byte{} {
		t.Errorf("Expected non empty hash on second message")
	}

}
