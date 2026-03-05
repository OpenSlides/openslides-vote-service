package vote

import (
	"context"
	"fmt"
)

// Reset removes all votes from a poll and sets its state to created.
func (v *Vote) Reset(ctx context.Context, pollID int, requestUserID int) error {
	poll, err := fetchPoll(ctx, v.flow, pollID)
	if err != nil {
		return fmt.Errorf("fetching poll: %w", err)
	}

	if err := canManagePoll(ctx, v.flow, poll.MeetingID, poll.ContentObjectID, requestUserID); err != nil {
		return fmt.Errorf("check permissions: %w", err)
	}

	tx, err := v.querier.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM poll WHERE id = $1)`, pollID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check poll existence: %w", err)
	}

	if !exists {
		return MessageErrorf(ErrInvalid, "Poll with id %d not found", pollID)
	}

	deleteVoteQuery := `DELETE FROM ballot WHERE poll_id = $1`
	if _, err := tx.Exec(ctx, deleteVoteQuery, pollID); err != nil {
		return fmt.Errorf("delete ballots: %w", err)
	}

	state := "created"
	if poll.Visibility == "manually" {
		state = "finished"
	}

	updateQuery := `UPDATE poll SET state = $1, published = false, result = '' WHERE id = $2`
	if _, err := tx.Exec(ctx, updateQuery, state, pollID); err != nil {
		return fmt.Errorf("reset poll state: %w", err)
	}

	deleteVotedQuery := `DELETE FROM nm_meeting_user_poll_voted_ids_poll_t WHERE poll_id = $1`
	if _, err := tx.Exec(ctx, deleteVotedQuery, pollID); err != nil {
		return fmt.Errorf("delete poll votes: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
