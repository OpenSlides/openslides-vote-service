package vote

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/OpenSlides/openslides-go/datastore/flow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Vote holds the state of the service.
//
// Vote has to be initializes with vote.New().
type Vote struct {
	flow    flow.Flow
	querier DBQuerier
}

// New creates an initializes vote service.
func New(ctx context.Context, flow flow.Flow, querier DBQuerier) (*Vote, func(context.Context, func(error)), error) {
	v := &Vote{
		flow:    flow,
		querier: querier,
	}

	bg := func(ctx context.Context, errorHandler func(error)) {
		// TODO: listen to state changes
		// For example, check for state changes and preload data, when a poll gets started.
		go v.flow.Update(ctx, nil)
	}

	return v, bg, nil
}

// Start validates a poll and set its state to started.
func (v *Vote) Start(ctx context.Context, pollID int, requestUserID int) error {
	ds := dsmodels.New(v.flow)

	// TODO: Check permissions for requestUser

	// TODO: If only poll.method is needed, don't load the full poll.
	poll, err := ds.Poll(pollID).First(ctx)
	if err != nil {
		var doesNotExist dsfetch.DoesNotExistError
		if errors.As(err, &doesNotExist) {
			return MessageErrorf(ErrNotExists, "Poll %d does not exist", pollID)
		}
		return fmt.Errorf("loading poll %d: %w", pollID, err)
	}

	if poll.Method == "analog" {
		return MessageError(ErrInvalid, "Analog poll can not be started")
	}

	if poll.State != "created" {
		return MessageErrorf(ErrInvalid, "Poll %d is not in the created state", pollID)
	}

	sql := `UPDATE poll SET state = 'started' WHERE id = $1 AND state = 'created';`
	commandTag, err := v.querier.Exec(ctx, sql, pollID)
	if err != nil {
		return fmt.Errorf("set poll %d to started: %w", pollID, err)
	}

	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("poll %d not found or not in 'created' state", pollID)
	}

	return nil
}

// Stop ends a poll. Creates poll/result
func (v *Vote) Stop(ctx context.Context, pollID int, requestUserID int) error {
	ds := dsmodels.New(v.flow)

	// TODO: Check permissions for requestUser

	// TODO: Maybe only some fields of the poll are required.
	poll, err := ds.Poll(pollID).First(ctx)
	if err != nil {
		var doesNotExist dsfetch.DoesNotExistError
		if errors.As(err, &doesNotExist) {
			return MessageErrorf(ErrNotExists, "Poll %d does not exist", pollID)
		}
		return fmt.Errorf("loading poll %d: %w", pollID, err)
	}

	if poll.State != "started" {
		return MessageErrorf(ErrInvalid, "Poll %d is not in the started state", pollID)
	}

	// TODO: Create "poll/result

	sql := `UPDATE poll SET state = 'finished' WHERE id = $1 AND state = 'started';`
	commandTag, err := v.querier.Exec(ctx, sql, pollID)
	if err != nil {
		return fmt.Errorf("set poll %d to finished: %w", pollID, err)
	}

	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("poll %d not found or not in 'finished' state", pollID)
	}

	return nil
}

// VoteResult enthält das Ergebnis der Vote-Operation
type VoteResult struct {
	Success bool   `json:"success"`
	Status  string `json:"status"`
	VoteID  *int   `json:"vote_id,omitempty"`
	Message string `json:"message"`
}

// Vote validates and saves the vote.
func (v *Vote) Vote(ctx context.Context, pollID, requestUserID int, r io.Reader) error {
	ds := dsmodels.New(v.flow)

	poll, err := ds.Poll(pollID).First(ctx)
	if err != nil {
		var doesNotExist dsfetch.DoesNotExistError
		if errors.As(err, &doesNotExist) {
			return MessageErrorf(ErrNotExists, "Poll %d does not exist", pollID)
		}
		return fmt.Errorf("loading poll %d: %w", pollID, err)
	}

	// TODO: Validate, dass represented User can vote, that the requestUser can
	// vote for him and that the vote is valid.
	// - Check that request user is present
	// - Read representedUser from body or use request user ID
	// - check, that non of them is anonymous and are part of the meeting.
	// - Validate the vote.value
	// - Set vote weight
	//

	meetingID := 404
	voteValue := "TODO"
	weight := "1.000000"
	actingUserID := requestUserID
	representedUserID := actingUserID

	// TODO: Maybe make this a function in the schema.
	sql := `WITH
		poll_check AS (
			SELECT
				id,
				state,
				CASE
					WHEN id IS NULL THEN 'POLL_NOT_FOUND'
					WHEN state != 'started' THEN 'POLL_NOT_STARTED'
					ELSE 'POLL_VALID'
				END as poll_status
			FROM poll
			WHERE id = $2
		),
		vote_check AS (
			SELECT
				COUNT(*) as existing_votes,
				CASE
					WHEN COUNT(*) > 0 THEN 'USER_HAS_VOTED_BEFORE'
					ELSE 'VOTE_OK'
				END as vote_status
			FROM vote
			WHERE poll_id = $2 AND represented_user_id = $6
		),
		inserted AS (
			INSERT INTO vote
			(meeting_id, poll_id, value, weight, acting_user_id, represented_user_id)
			SELECT $1, $2, $3, $4, $5, $6
			FROM poll_check p, vote_check v
			WHERE p.poll_status = 'POLL_VALID' AND v.vote_status = 'VOTE_OK'
			RETURNING id
		)
		SELECT
			CASE
				WHEN i.id IS NOT NULL THEN 'VALID'
				WHEN p.poll_status != 'POLL_VALID' THEN p.poll_status
				WHEN v.vote_status != 'VOTE_OK' THEN v.vote_status
				ELSE 'UNKNOWN_ERROR'
			END as status
		FROM poll_check p, vote_check v
		LEFT JOIN inserted i ON true;`

	var status string
	err = v.querier.QueryRow(ctx, sql, meetingID, pollID, voteValue, weight, actingUserID, representedUserID).Scan(
		&status,
	)
	if err != nil {
		return fmt.Errorf("failed to insert vote: %w", err)
	}

	switch status {
	case "VALID":
		return nil
	case "POLL_NOT_FOUND":
		return MessageErrorf(ErrNotExists, "Poll %d does not exist", pollID)
	case "POLL_NOT_STARTED":
		return MessageErrorf(ErrNotStarted, "Poll %d is not started", poll)
	case "USER_HAS_VOTED_BEFORE":
		return MessageErrorf(ErrDoubleVote, "You can not vote again on poll %d", pollID)
	default:
		return fmt.Errorf("unknown vote sql status %s", status)
	}
}
