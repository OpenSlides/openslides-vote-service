package vote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dskey"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/OpenSlides/openslides-go/datastore/flow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBQuerier interface {
	Begin(ctx context.Context) (pgx.Tx, error)
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

// Creates a poll, returning the poll id.
func (v *Vote) Create(ctx context.Context, requestUserID int, r io.Reader) (int, error) {
	// TODO: Check permissions for requestUser

	ci, err := parseCreateInput(ctx, v.flow, r)
	if err != nil {
		return 0, fmt.Errorf("parsing input: %w", err)
	}

	// TODO: Check organization/1/enable_electronic_voting

	tx, err := v.querier.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var sequentialNumber int
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(sequential_number), 0)
		FROM poll
		WHERE meeting_id = $1
		FOR UPDATE`, ci.MeetingID).Scan(&sequentialNumber)

	if err != nil {
		return 0, fmt.Errorf("get max sequential number: %w", err)
	}

	sequentialNumber += 1

	sql := `INSERT INTO poll
		(title, desciption, method, config, visibility, state, sequential_number, content_object_id, entitled_group_ids, meeting_id)
		VALUES ($1, $2, $3, $4, $5, 'created', $6, $7, $8, $9)
		RETURNING id;`

	var newID int
	if err := tx.QueryRow(
		ctx,
		sql,
		ci.Title,
		ci.Description,
		ci.Method,
		ci.Config,
		ci.Visibility,
		sequentialNumber,
		ci.ContentObjectID,
		ci.EntitledGroupIDs,
		ci.MeetingID).Scan(&newID); err != nil {
		return 0, fmt.Errorf("save poll: %w", err)
	}

	// 4. Transaktion committen
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return newID, nil
}

type CreateInput struct {
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	ContentObjectID  string          `json:"content_object_id"`
	MeetingID        int             `json:"meeting_id"`
	Method           string          `json:"method"`
	Config           json.RawMessage `json:"config"`
	Visibility       string          `json:"visibility"`
	EntitledGroupIDs []int           `json:"entitled_group_ids"`
}

func parseCreateInput(ctx context.Context, ds flow.Getter, r io.Reader) (CreateInput, error) {
	var ci CreateInput
	if err := json.NewDecoder(r).Decode(&ci); err != nil {
		return CreateInput{}, fmt.Errorf("reading json: %w", err)
	}

	// TODO: Is everything else realy necessary, or can I trust postgres?

	if ci.Title == "" {
		return CreateInput{}, MessageError(ErrInvalid, "Title can not be empty")
	}

	if err := validateContentObjectID(ctx, ci.ContentObjectID, ds); err != nil {
		return CreateInput{}, fmt.Errorf("validate content_object_id: %w", err)
	}

	if ci.MeetingID == 0 {
		// TODO: Should I check, that the meeing exists and that content_object_id is in it?
		return CreateInput{}, MessageError(ErrInvalid, "Meeting ID is required")
	}

	// TODO: Is it possible to auto generate this without inporting meta here?
	methodValues := []string{"analog", "motion", "selection", "rating", "single_transferable_vote"}
	if !slices.Contains(methodValues, ci.Method) {
		return CreateInput{}, MessageErrorf(ErrInternal, "Method has to be one of %v, not %s", methodValues, ci.Method)
	}

	// TODO: check config and visiblity

	return ci, nil

}

func validateContentObjectID(ctx context.Context, contentObjectID string, ds flow.Getter) error {
	key, err := dskey.FromStringf("%s/id", context.Canceled)
	if err != nil {
		return MessageError(ErrInvalid, "Invalid content_object_id")
	}

	if _, err := ds.Get(ctx, key); err != nil {
		// TODO: Check for does not exist error and return MessageError
		return fmt.Errorf("checking content object: %w", err)
	}

	return nil
}

func (v *Vote) Update(ctx context.Context, pollID int, requestUserID int) error {
	return errors.New("TODO")
}

func (v *Vote) Delete(ctx context.Context, pollID int, requestUserID int) error {
	// TODO: Check permissions
	sql := `DELETE FROM poll WHERE poll_id = $1;`
	if _, err := v.querier.Exec(ctx, sql, pollID); err != nil {
		return fmt.Errorf("sending sql query: %w", err)
	}
	return nil
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

func (v *Vote) Publish(ctx context.Context, pollID int, requestUserID int) error {
	// TODO: Check permissions

	sql := `UPDATE poll
			SET state = 'published'
			WHERE id = $1 AND state = 'finished'`

	result, err := v.querier.Exec(ctx, sql, pollID)
	if err != nil {
		return fmt.Errorf("update poll state: %w", err)
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		return MessageErrorf(ErrInvalid, "Poll with id %d not found or not in 'finished' state", pollID)
	}

	return nil
}

func (v *Vote) Anonymize(ctx context.Context, pollID int, requestUserID int) error {
	// TODO: Check permissions

	tx, err := v.querier.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var pollState string
	err = tx.QueryRow(ctx, `SELECT state FROM poll WHERE id = $1`, pollID).Scan(&pollState)
	if err != nil {
		if err == pgx.ErrNoRows {
			return MessageErrorf(ErrInvalid, "Poll with id %d not found", pollID)
		}
		return fmt.Errorf("check poll state: %w", err)
	}

	if pollState != "finished" && pollState != "published" {
		return MessageErrorf(ErrInvalid, "Poll with id %d is not in 'finished' or 'published' state (current: %s)", pollID, pollState)
	}

	sql := `UPDATE vote
			SET acting_user_id = NULL, represented_user_id = NULL
			WHERE poll_id = $1`

	if _, err := tx.Exec(ctx, sql, pollID); err != nil {
		return fmt.Errorf("anonymize votes: %w", err)
	}

	// Transaktion committen
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (v *Vote) Reset(ctx context.Context, pollID int, requestUserID int) error {
	// TODO: Check permissions

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

	deleteSQL := `DELETE FROM vote WHERE poll_id = $1`
	if _, err := tx.Exec(ctx, deleteSQL, pollID); err != nil {
		return fmt.Errorf("delete votes: %w", err)
	}

	updateSQL := `UPDATE poll SET state = 'created' WHERE id = $1`

	if _, err := tx.Exec(ctx, updateSQL, pollID); err != nil {
		return fmt.Errorf("reset poll state: %w", err)
	}

	// Transaktion committen
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
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
