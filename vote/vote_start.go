package vote

import (
	"context"
	"fmt"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
)

// Start validates a poll and set its state to started.
func (v *Vote) Start(ctx context.Context, pollID int, requestUserID int) error {
	poll, err := fetchPoll(ctx, v.flow, pollID)
	if err != nil {
		return fmt.Errorf("fetching poll: %w", err)
	}

	if err := canManagePoll(ctx, v.flow, poll.MeetingID, poll.ContentObjectID, requestUserID); err != nil {
		return fmt.Errorf("check permissions: %w", err)
	}

	if poll.State == "finished" {
		return MessageErrorf(ErrInvalid, "Poll %d is already finished", pollID)
	}

	if err := Preload(ctx, dsfetch.New(v.flow), poll.ID, poll.MeetingID); err != nil {
		return fmt.Errorf("preloading poll: %w", err)
	}

	sql := `UPDATE poll SET state = 'started' WHERE id = $1 AND state != 'finished';`
	commandTag, err := v.querier.Exec(ctx, sql, pollID)
	if err != nil {
		return fmt.Errorf("set poll %d to started: %w", pollID, err)
	}

	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("poll %d not found or not in 'created' state", pollID)
	}

	return nil
}
