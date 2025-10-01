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
	"github.com/OpenSlides/openslides-go/perm"
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
	electronicVotingEnabled, err := dsfetch.New(v.flow).Organization_EnableElectronicVoting(1).Value(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetch organization/1/enable_electronic_voting: %w", err)
	}

	ci, err := parseCreateInput(r, electronicVotingEnabled)
	if err != nil {
		return 0, fmt.Errorf("parsing input: %w", err)
	}

	if err := canManagePoll(ctx, v.flow, ci.MeetingID, ci.ContentObjectID, requestUserID); err != nil {
		return 0, fmt.Errorf("check permissions: %w", err)
	}

	tx, err := v.querier.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// TODO: Can be removed afer https://github.com/OpenSlides/openslides-meta/issues/219 is fixed.
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
		(title, method, config, visibility, state, sequential_number, content_object_id, meeting_id, result, published)
		VALUES ($1, $2, $3, $4, 'created', $5, $6, $7, $8, $9)
		RETURNING id;`

	var newID int
	if err := tx.QueryRow(
		ctx,
		sql,
		ci.Title,
		ci.Method,
		ci.Config,
		ci.Visibility,
		sequentialNumber,
		ci.ContentObjectID,
		ci.MeetingID,
		string(ci.Result),
		ci.Published,
	).Scan(&newID); err != nil {
		return 0, fmt.Errorf("save poll: %w", err)
	}

	if len(ci.EntitledGroupIDs) > 0 {
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
	ContentObjectID  string          `json:"content_object_id"`
	MeetingID        int             `json:"meeting_id"`
	Method           string          `json:"method"`
	Config           json.RawMessage `json:"config"`
	Visibility       string          `json:"visibility"`
	EntitledGroupIDs []int           `json:"entitled_group_ids"`
	Published        bool            `json:"published"`
	Result           json.RawMessage `json:"result"`
}

func parseCreateInput(r io.Reader, electronicVotingEnabled bool) (CreateInput, error) {
	var ci CreateInput
	if err := json.NewDecoder(r).Decode(&ci); err != nil {
		return CreateInput{}, fmt.Errorf("reading json: %w", err)
	}

	if ci.Title == "" {
		return CreateInput{}, MessageError(ErrInvalid, "Title can not be empty")
	}

	if ci.ContentObjectID == "" {
		return CreateInput{}, MessageError(ErrInvalid, "Content Object ID can not be empty")
	}

	if ci.MeetingID == 0 {
		return CreateInput{}, MessageError(ErrInvalid, "Meeting ID can not be empty")
	}

	if ci.Method == "" {
		return CreateInput{}, MessageError(ErrInvalid, "Method can not be empty")
	}

	if ci.Visibility == "" {
		return CreateInput{}, MessageError(ErrInvalid, "Visibility can not be empty")
	}

	switch ci.Visibility {
	case "manually":
		if len(ci.EntitledGroupIDs) > 0 {
			return CreateInput{}, MessageError(ErrInvalid, "Entitled Group IDs can not be set when visibility is set to manually")
		}

	default:
		if !electronicVotingEnabled {
			return CreateInput{}, MessageError(ErrNotAllowed, "Electronic voting is not enabled. Only polls with visibility set to manually are allowed.")
		}

		if ci.Result != nil {
			return CreateInput{}, MessageError(ErrInvalid, "Result can only be set when visibility is set to manually")
		}
	}

	if err := ValidateConfig(ci.Method, string(ci.Config)); err != nil {
		return CreateInput{}, fmt.Errorf("validate config: %w", err)
	}

	return ci, nil
}

func (v *Vote) Update(ctx context.Context, pollID int, requestUserID int, r io.Reader) error {
	poll, err := fetchPoll(ctx, v.flow, pollID)
	if err != nil {
		return fmt.Errorf("fetching poll: %w", err)
	}

	if err := canManagePoll(ctx, v.flow, poll.MeetingID, poll.ContentObjectID, requestUserID); err != nil {
		return fmt.Errorf("check permissions: %w", err)
	}

	electronicVotingEnabled, err := dsfetch.New(v.flow).Organization_EnableElectronicVoting(1).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetch organization/1/enable_electronic_voting: %w", err)
	}

	ui, err := parseUpdateInput(r, poll, electronicVotingEnabled)
	if err != nil {
		return fmt.Errorf("parse update body: %w", err)
	}

	tx, err := v.querier.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	sql, values := ui.createQuery(pollID)
	if len(values) > 0 {
		if _, err := tx.Exec(ctx, sql, values...); err != nil {
			return fmt.Errorf("update poll: %w", err)
		}
	}

	if len(ui.EntitledGroupIDs) > 0 {
		sql := "DELETE FROM nm_group_poll_ids_poll_t WHERE poll_id = $1"
		if _, err := tx.Exec(ctx, sql, pollID); err != nil {
			return fmt.Errorf("deleting existing group associations: %w", err)
		}

		placeholders := make([]string, len(ui.EntitledGroupIDs))
		args := make([]any, len(ui.EntitledGroupIDs)*2)

		for i, groupID := range ui.EntitledGroupIDs {
			placeholders[i] = fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2)
			args[i*2] = groupID
			args[i*2+1] = poll.ID
		}

		groupSQL := fmt.Sprintf(
			"INSERT INTO nm_group_poll_ids_poll_t (group_id, poll_id) VALUES %s",
			strings.Join(placeholders, ", "),
		)

		if _, err := tx.Exec(ctx, groupSQL, args...); err != nil {
			return fmt.Errorf("insert group-poll relations: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

type UpdateInput struct {
	Title            string              `json:"title"`
	Method           string              `json:"method"`
	Config           json.RawMessage     `json:"config"`
	Visibility       string              `json:"visibility"`
	EntitledGroupIDs []int               `json:"entitled_group_ids"`
	Published        dsfetch.Maybe[bool] `json:"published"`
	Result           json.RawMessage     `json:"result"`
}

func parseUpdateInput(r io.Reader, poll dsmodels.Poll, electronicVotingEnabled bool) (UpdateInput, error) {
	var ui UpdateInput
	if err := json.NewDecoder(r).Decode(&ui); err != nil {
		return UpdateInput{}, fmt.Errorf("decoding update input: %w", err)
	}

	if poll.State != "created" {
		if ui.Method != "" {
			return UpdateInput{}, MessageError(ErrNotAllowed, "method can only be changed before the poll has started")
		}

		if ui.Config != nil {
			return UpdateInput{}, MessageError(ErrNotAllowed, "config can only be changed before the poll has started")
		}

		if ui.Visibility != "" {
			return UpdateInput{}, MessageError(ErrNotAllowed, "visibility can only be changed before the poll has started")
		}

		if ui.EntitledGroupIDs != nil {
			return UpdateInput{}, MessageError(ErrNotAllowed, "entitled group ids can only be changed before the poll has started")
		}
	}

	visibility := poll.Visibility
	if ui.Visibility != "" {
		visibility = ui.Visibility
	}

	switch visibility {
	case "manually":
		if len(ui.EntitledGroupIDs) > 0 {
			return UpdateInput{}, MessageError(ErrNotAllowed, "Entitled Group IDs can not be set when visibility is set to manually")
		}

	default:
		if !electronicVotingEnabled {
			return UpdateInput{}, MessageError(ErrNotAllowed, "Electronic voting is not enabled. Only polls with visibility set to manually are allowed.")
		}

		if ui.Result != nil {
			return UpdateInput{}, MessageError(ErrNotAllowed, "Result can only be set when visibility is set to manually")
		}
	}

	if ui.Config != nil {
		method := poll.Method
		if ui.Method != "" {
			method = ui.Method
		}

		if err := ValidateConfig(method, string(ui.Config)); err != nil {
			return UpdateInput{}, fmt.Errorf("validate config: %w", err)
		}
	}

	return ui, nil
}

func (ui UpdateInput) createQuery(pollID int) (string, []any) {
	var setParts []string
	var args []any
	argIndex := 1

	if ui.Title != "" {
		setParts = append(setParts, fmt.Sprintf("title = $%d", argIndex))
		args = append(args, ui.Title)
		argIndex++
	}

	if ui.Method != "" {
		setParts = append(setParts, fmt.Sprintf("method = $%d", argIndex))
		args = append(args, ui.Method)
		argIndex++
	}

	if ui.Config != nil {
		setParts = append(setParts, fmt.Sprintf("config = $%d", argIndex))
		args = append(args, string(ui.Config))
		argIndex++
	}

	if ui.Visibility != "" {
		setParts = append(setParts, fmt.Sprintf("visibility = $%d", argIndex))
		args = append(args, ui.Visibility)
		argIndex++
	}

	if published, hasValue := ui.Published.Value(); hasValue {
		setParts = append(setParts, fmt.Sprintf("published = $%d", argIndex))
		args = append(args, published)
		argIndex++
	}

	if ui.Result != nil {
		setParts = append(setParts, fmt.Sprintf("result = $%d", argIndex))
		args = append(args, string(ui.Result))
		argIndex++
	}

	if len(setParts) == 0 {
		return "", nil
	}

	query := fmt.Sprintf("UPDATE poll SET %s WHERE id = $%d",
		strings.Join(setParts, ", "),
		argIndex)

	args = append(args, pollID)

	return query, args
}

func (v *Vote) Delete(ctx context.Context, pollID int, requestUserID int) error {
	poll, err := fetchPoll(ctx, v.flow, pollID)
	if err != nil {
		return fmt.Errorf("fetching poll: %w", err)
	}

	if err := canManagePoll(ctx, v.flow, poll.MeetingID, poll.ContentObjectID, requestUserID); err != nil {
		return fmt.Errorf("check permissions: %w", err)
	}

	sql := `DELETE FROM poll WHERE id = $1;`
	if _, err := v.querier.Exec(ctx, sql, pollID); err != nil {
		return fmt.Errorf("sending sql query: %w", err)
	}
	return nil
}

// Start validates a poll and set its state to started.
func (v *Vote) Start(ctx context.Context, pollID int, requestUserID int) error {
	poll, err := fetchPoll(ctx, v.flow, pollID)
	if err != nil {
		return fmt.Errorf("fetching poll: %w", err)
	}

	if err := canManagePoll(ctx, v.flow, poll.MeetingID, poll.ContentObjectID, requestUserID); err != nil {
		return fmt.Errorf("check permissions: %w", err)
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

// Finalize ends a poll.
//
// - If in the started state, it creates poll/result.
// - Sets the state to `finished`.
// - Sets the `published` flag.
// - With the flag `anonymize`, clears all user_ids from the coresponding votes.
func (v *Vote) Finalize(ctx context.Context, pollID int, requestUserID int, publish bool, anonymize bool) error {
	poll, err := fetchPoll(ctx, v.flow, pollID)
	if err != nil {
		return fmt.Errorf("fetching poll: %w", err)
	}

	if err := canManagePoll(ctx, v.flow, poll.MeetingID, poll.ContentObjectID, requestUserID); err != nil {
		return fmt.Errorf("check permissions: %w", err)
	}

	if poll.State == "created" {
		// TODO: What abount anually polls an publish flag?
		return MessageErrorf(ErrInvalid, "Poll %d has not started yet.", pollID)
	}

	tx, err := v.querier.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if poll.State == `started` {
		votes := poll.VoteList
		result, err := CreateResult(poll.Method, poll.Config, votes)
		if err != nil {
			return fmt.Errorf("create poll result: %w", err)
		}

		sql := `UPDATE poll SET result = $1 WHERE id = $2;`
		if _, err := tx.Exec(ctx, sql, result, pollID); err != nil {
			return fmt.Errorf("set result of poll %d: %w", pollID, err)
		}
	}

	sql := `UPDATE poll SET state = 'finished', published = $1 WHERE id = $2;`
	if _, err := tx.Exec(ctx, sql, publish, pollID); err != nil {
		return fmt.Errorf("set poll %d to finished and publish to %v: %w", pollID, publish, err)
	}

	if anonymize {
		sql := `UPDATE vote
				SET acting_user_id = NULL, represented_user_id = NULL
				WHERE poll_id = $1`

		if _, err := tx.Exec(ctx, sql, pollID); err != nil {
			return fmt.Errorf("anonymize votes: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

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

	deleteSQL := `DELETE FROM vote WHERE poll_id = $1`
	if _, err := tx.Exec(ctx, deleteSQL, pollID); err != nil {
		return fmt.Errorf("delete votes: %w", err)
	}

	updateSQL := `UPDATE poll SET state = 'created', published = false WHERE id = $1`

	if _, err := tx.Exec(ctx, updateSQL, pollID); err != nil {
		return fmt.Errorf("reset poll state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

type VoteResult struct {
	Success bool   `json:"success"`
	Status  string `json:"status"`
	VoteID  *int   `json:"vote_id,omitempty"`
	Message string `json:"message"`
}

// Vote validates and saves the vote.
func (v *Vote) Vote(ctx context.Context, pollID, requestUserID int, r io.Reader) error {
	if requestUserID == 0 {
		return MessageErrorf(ErrInvalid, "Anonymous can not vote")
	}

	poll, err := fetchPoll(ctx, v.flow, pollID)
	if err != nil {
		return fmt.Errorf("fetching poll: %w", err)
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

	fetch := dsfetch.New(v.flow)
	if err := allowedToVote(ctx, fetch, poll, actingUserID, representedUserID, poll.MeetingID); err != nil {
		return fmt.Errorf("allowedToVote: %w", err)
	}

	if !poll.AllowInvalid {
		if err := ValidateVote(poll.Method, poll.Config, body.Value); err != nil {
			return fmt.Errorf("validate vote: %w", err)
		}
	}

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

func ValidateConfig(method string, config string) error {
	switch method {
	case methodMotion{}.Name():
		return methodMotion{}.ValidateConfig(config)
	case methodSelection{}.Name():
		return methodSelection{}.ValidateConfig(config)
	case methodRating{}.Name():
		return methodRating{}.ValidateConfig(config)
	case methodRatingMotion{}.Name():
		return methodRatingMotion{}.ValidateConfig(config)
	default:
		return MessageErrorf(ErrInvalid, "Unknown poll method: %s", method)
	}
}

func ValidateVote(method string, config string, vote json.RawMessage) error {
	switch method {
	case methodMotion{}.Name():
		return methodMotion{}.ValidateVote(config, vote)
	case methodSelection{}.Name():
		return methodSelection{}.ValidateVote(config, vote)
	case methodRating{}.Name():
		return methodRating{}.ValidateVote(config, vote)
	case methodRatingMotion{}.Name():
		return methodRatingMotion{}.ValidateVote(config, vote)
	default:
		return fmt.Errorf("unknown poll method: %s", method)
	}
}

func CreateResult(method string, config string, votes []dsmodels.Vote) (string, error) {
	switch method {
	case methodMotion{}.Name():
		return methodMotion{}.Result(config, votes)
	case methodSelection{}.Name():
		return methodSelection{}.Result(config, votes)
	case methodRating{}.Name():
		return methodRating{}.Result(config, votes)
	case methodRatingMotion{}.Name():
		return methodRatingMotion{}.Result(config, votes)
	default:
		return "", fmt.Errorf("unknown poll method: %s", method)
	}
}

func fetchPoll(ctx context.Context, getter flow.Getter, pollID int) (dsmodels.Poll, error) {
	ds := dsmodels.New(getter)
	poll, err := ds.Poll(pollID).First(ctx)
	if err != nil {
		var doesNotExist dsfetch.DoesNotExistError
		if errors.As(err, &doesNotExist) {
			return dsmodels.Poll{}, MessageErrorf(ErrNotExists, "Poll %d does not exist", pollID)
		}
		return dsmodels.Poll{}, fmt.Errorf("loading poll %d: %w", pollID, err)
	}

	return poll, nil
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

func canManagePoll(ctx context.Context, getter flow.Getter, meetingID int, contentObjectID string, userID int) error {
	collection, _, found := strings.Cut(contentObjectID, "/")
	if !found {
		return fmt.Errorf("invalid content object id: %s", contentObjectID)
	}

	var requiredPerm perm.TPermission
	switch collection {
	case "motion":
		requiredPerm = perm.MotionCanManagePolls
	case "assignment":
		requiredPerm = perm.AssignmentCanManagePolls
	case "topic":
		requiredPerm = perm.PollCanManage
	default:
		return fmt.Errorf(
			"invalid content object id %s, only motion, assignment or topic allowed",
			contentObjectID,
		)
	}

	userPerms, err := perm.New(ctx, dsfetch.New(getter), userID, meetingID)
	if err != nil {
		return fmt.Errorf("calculate user permissions: %w", err)
	}

	if !userPerms.Has(requiredPerm) {
		return MessageError(ErrNotAllowed, "You are not allowed to manage a poll")
	}

	return nil
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
