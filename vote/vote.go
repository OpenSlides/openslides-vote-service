package vote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
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

	ci, err := parseCreateInput(r)
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
		SELECT sequential_number
		FROM poll
		WHERE meeting_id = $1
		ORDER BY sequential_number DESC
		LIMIT 1
		FOR UPDATE`, ci.MeetingID).Scan(&sequentialNumber)

	if err != nil {
		if err != pgx.ErrNoRows {
			return 0, fmt.Errorf("get max sequential number: %w", err)
		}
	}

	sequentialNumber += 1

	sql := `INSERT INTO poll
		(title, description, method, config, visibility, state, sequential_number, content_object_id, meeting_id)
		VALUES ($1, $2, $3, $4, $5, 'created', $6, $7, $8)
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
		ci.MeetingID).Scan(&newID); err != nil {
		return 0, fmt.Errorf("save poll: %w", err)
	}

	if len(ci.EntitledGroupIDs) > 0 {
		// Dynamisches SQL für alle IDs auf einmal
		placeholders := make([]string, len(ci.EntitledGroupIDs))
		args := make([]any, len(ci.EntitledGroupIDs)*2)

		for i, groupID := range ci.EntitledGroupIDs {
			placeholders[i] = fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2)
			args[i*2] = groupID
			args[i*2+1] = newID
		}

		groupSQL := fmt.Sprintf(
			"INSERT INTO nm_group_poll_ids_poll_t (group_id, poll_id) VALUES %s",
			strings.Join(placeholders, ", "),
		)

		if _, err := tx.Exec(ctx, groupSQL, args...); err != nil {
			return 0, fmt.Errorf("insert group-poll relations: %w", err)
		}
	}

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

func parseCreateInput(r io.Reader) (CreateInput, error) {
	var ci CreateInput
	if err := json.NewDecoder(r).Decode(&ci); err != nil {
		return CreateInput{}, fmt.Errorf("reading json: %w", err)
	}

	if ci.Title == "" {
		return CreateInput{}, MessageError(ErrInvalid, "Title can not be empty")
	}

	// TODO: check config and visiblity

	return ci, nil

}

func (v *Vote) Update(ctx context.Context, pollID int, requestUserID int) error {
	return errors.New("TODO")
}

func (v *Vote) Delete(ctx context.Context, pollID int, requestUserID int) error {
	// TODO: Check permissions
	sql := `DELETE FROM poll WHERE id = $1;`
	if _, err := v.querier.Exec(ctx, sql, pollID); err != nil {
		return fmt.Errorf("sending sql query: %w", err)
	}
	return nil
}

// Start validates a poll and set its state to started.
func (v *Vote) Start(ctx context.Context, pollID int, requestUserID int) error {
	ds := dsmodels.New(v.flow)

	// TODO: Check permissions for requestUser

	poll, err := ds.Poll(pollID).First(ctx)
	if err != nil {
		var doesNotExist dsfetch.DoesNotExistError
		if errors.As(err, &doesNotExist) {
			return MessageErrorf(ErrNotExists, "Poll %d does not exist", pollID)
		}
		return fmt.Errorf("loading poll %d: %w", pollID, err)
	}

	if poll.Visibility == "manually" {
		return MessageError(ErrInvalid, "Manually poll can not be started")
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
			WHERE id = $1 AND state = 'finished';`

	result, err := v.querier.Exec(ctx, sql, pollID)
	if err != nil {
		return fmt.Errorf("update poll/%d/state: %w", pollID, err)
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
	fetch := dsfetch.New(v.flow)
	dsmodel := dsmodels.New(v.flow)

	if requestUserID == 0 {
		return MessageErrorf(ErrInvalid, "Anonymous can not vote")
	}

	poll, err := dsmodel.Poll(pollID).First(ctx)
	if err != nil {
		var doesNotExist dsfetch.DoesNotExistError
		if errors.As(err, &doesNotExist) {
			return MessageErrorf(ErrNotExists, "Poll %d does not exist", pollID)
		}
		return fmt.Errorf("loading poll %d: %w", pollID, err)
	}

	var body struct {
		UserID int             `json:"user_id"`
		Value  json.RawMessage `json:"value"`
	}

	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return fmt.Errorf("decoding body: %w", err)
	}

	actingUserID := requestUserID
	representedUserID := actingUserID
	if body.UserID != 0 {
		representedUserID = body.UserID
	}

	if err := allowedToVote(ctx, fetch, poll, actingUserID, representedUserID, poll.MeetingID); err != nil {
		return fmt.Errorf("allowedToVote: %w", err)
	}

	// TODO:
	// - Validate the vote.value
	// - Set vote weight

	meetingID := poll.MeetingID
	voteValue := string(body.Value)
	weight, err := CalcVoteWeight(ctx, fetch, meetingID, representedUserID)
	if err != nil {
		return fmt.Errorf("calc vote weight: %w", err)
	}

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
		return MessageErrorf(ErrNotStarted, "Poll %d is not started", pollID)
	case "USER_HAS_VOTED_BEFORE":
		return MessageErrorf(ErrDoubleVote, "You can not vote again on poll %d", pollID)
	default:
		return fmt.Errorf("unknown vote sql status %s", status)
	}
}

// allowedToVote checks, that the represented user can vote and the acting user
// can vote for him.
func allowedToVote(
	ctx context.Context,
	ds *dsfetch.Fetch,
	poll dsmodels.Poll,
	representedUserID int,
	actingUserID int,
	meetingID int,
) error {
	if err := ensurePresent(ctx, ds, meetingID, actingUserID); err != nil {
		return fmt.Errorf("ensure acting user is present: %w", err)
	}

	representedMeetingUserID, found, err := getMeetingUser(ctx, ds, representedUserID, meetingID)
	if err != nil {
		return fmt.Errorf("getting meeting user for represented user: %w", err)
	}
	if !found {
		return fmt.Errorf("represented user does not have a meeting user, althought he is present")
	}

	groupIDs, err := ds.MeetingUser_GroupIDs(representedMeetingUserID).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetching groups of user %d in meeting %d: %w", representedUserID, poll.MeetingID, err)
	}

	if !hasCommon(groupIDs, poll.EntitledGroupIDs) {
		return MessageErrorf(ErrNotAllowed, "User %d is not allowed to vote. He is not in an entitled group", representedUserID)
	}

	delegationActivated, err := ds.Meeting_UsersEnableVoteDelegations(poll.MeetingID).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetching meeting/user_enable_vote_delegations: %w", err)
	}

	forbitDelegateToVote, err := ds.Meeting_UsersForbidDelegatorToVote(poll.MeetingID).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetching meeting/users_forbid_delegator_to_vote: %w", err)
	}

	delegation, err := ds.MeetingUser_VoteDelegatedToID(representedMeetingUserID).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetching meeting_user/vote_delegated_to_id: %w", err)
	}

	if delegationActivated && forbitDelegateToVote && !delegation.Null() && representedUserID == actingUserID {
		return MessageError(ErrNotAllowed, "You have delegated your vote and therefore can not vote for your self")
	}

	if representedUserID == actingUserID {
		return nil
	}

	if !delegationActivated {
		return MessageErrorf(ErrNotAllowed, "Vote delegation is not activated in meeting %d", poll.MeetingID)
	}

	actingMeetingUserID, found, err := getMeetingUser(ctx, ds, actingUserID, poll.MeetingID)
	if err != nil {
		return fmt.Errorf("getting meeting_user for acting user: %w", err)
	}
	if !found {
		return MessageError(ErrNotAllowed, "You are not in the right meeting")
	}

	if id, ok := delegation.Value(); !ok || id != actingMeetingUserID {
		return MessageErrorf(ErrNotAllowed, "You can not vote for user %d", representedUserID)
	}

	return nil
}

// CalcVoteWeight calculates the vote weight for a user in a meeting.
//
// voteweight is a DecimalField with 6 zeros.
func CalcVoteWeight(ctx context.Context, fetch *dsfetch.Fetch, meetingID int, userID int) (string, error) {
	const defaultVoteWeight = "1.000000"

	meetingUserID, found, err := getMeetingUser(ctx, fetch, userID, meetingID)
	if err != nil {
		return "", fmt.Errorf("getting meeting user: %w", err)
	}
	if !found {
		return "", fmt.Errorf("user %d has no meeting_user in meeting %d", userID, meetingID)
	}

	var voteWeightEnabled bool
	var meetingUserVoteWeight string
	var userDefaultVoteWeight string
	fetch.Meeting_UsersEnableVoteWeight(meetingID).Lazy(&voteWeightEnabled)
	fetch.MeetingUser_VoteWeight(meetingUserID).Lazy(&meetingUserVoteWeight)
	fetch.User_DefaultVoteWeight(userID).Lazy(&userDefaultVoteWeight)

	if err := fetch.Execute(ctx); err != nil {
		return "", fmt.Errorf("getting vote weight values from db: %w", err)
	}

	if !voteWeightEnabled {
		return defaultVoteWeight, nil
	}

	if meetingUserVoteWeight != "" {
		return meetingUserVoteWeight, nil
	}

	if userDefaultVoteWeight != "" {
		return userDefaultVoteWeight, nil
	}

	return defaultVoteWeight, nil
}

func getMeetingUser(ctx context.Context, fetch *dsfetch.Fetch, userID, meetingID int) (int, bool, error) {
	meetingUserIDs, err := fetch.User_MeetingUserIDs(userID).Value(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("getting all meeting_user ids: %w", err)
	}

	meetingIDs := make([]int, len(meetingUserIDs))
	for i := range meetingUserIDs {
		fetch.MeetingUser_MeetingID(meetingUserIDs[i]).Lazy(&meetingIDs[i])
	}

	if err := fetch.Execute(ctx); err != nil {
		return 0, false, fmt.Errorf("get all meeting IDs: %w", err)
	}

	idx := slices.Index(meetingIDs, meetingID)
	if idx == -1 {
		return 0, false, nil
	}

	return meetingUserIDs[idx], true, nil
}

func ensurePresent(ctx context.Context, ds *dsfetch.Fetch, meetingID, user int) error {
	presentMeetings, err := ds.User_IsPresentInMeetingIDs(user).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetching is present in meetings: %w", err)
	}

	if !slices.Contains(presentMeetings, meetingID) {
		return MessageErrorf(ErrNotAllowed, "You have to be present in meeting %d", meetingID)
	}

	return nil
}

func hasCommon(list1, list2 []int) bool {
	return slices.ContainsFunc(list1, func(a int) bool {
		return slices.Contains(list2, a)
	})
}
