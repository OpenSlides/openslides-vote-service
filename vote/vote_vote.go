package vote

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/shopspring/decimal"
)

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
		MeetingUserID dsfetch.Maybe[int] `json:"meeting_user_id"`
		Value         json.RawMessage    `json:"value"`
		Split         bool               `json:"split"`
	}

	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return MessageError(ErrInvalid, "Invalid body")
	}

	if body.Split && !poll.AllowVoteSplit {
		return MessageErrorf(ErrInvalid, "Vote split is not allowed for poll %d", poll.ID)
	}

	fetch := dsfetch.New(v.flow)
	actingMeetingUserID, found, err := getMeetingUser(ctx, fetch, requestUserID, poll.MeetingID)
	if err != nil {
		return fmt.Errorf("getting meeting user of request user: %w", err)
	}
	if !found {
		return MessageErrorf(ErrInvalid, "You have to be in the meeting to vote")
	}

	representedMeetingUserID := actingMeetingUserID
	if meetingUserID, set := body.MeetingUserID.Value(); set {
		representedMeetingUserID = meetingUserID
	}

	if err := allowedToVote(ctx, fetch, poll, representedMeetingUserID, actingMeetingUserID); err != nil {
		return fmt.Errorf("allowedToVote: %w", err)
	}

	ballotValue := string(body.Value)
	if poll.Visibility == "secret" {
		ballotValue, err = v.encryptBallot(ballotValue)
		if err != nil {
			return fmt.Errorf("encrypting ballot value: %w", err)
		}
	}

	weight, err := CalcVoteWeight(ctx, fetch, representedMeetingUserID)
	if err != nil {
		return fmt.Errorf("calc vote weight: %w", err)
	}

	if !poll.AllowInvalid {
		splitted := map[decimal.Decimal]json.RawMessage{decimal.Zero: body.Value}

		if body.Split {
			splitted, err = split(weight, body.Value)
			if err != nil {
				return fmt.Errorf("split vote: %w", err)
			}
		}

		method := pollMethod(poll)
		config, err := v.EncodeConfig(ctx, poll)
		if err != nil {
			return fmt.Errorf("encode config: %w", err)
		}

		for _, value := range splitted {
			if err := ValidateBallot(method, config, value); err != nil {
				return fmt.Errorf("validate ballot: %w", err)
			}
		}
	}

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
			WHERE id = $1
		),
		ballot_check AS (
			SELECT
				COUNT(*) as existing_ballots,
				CASE
					WHEN COUNT(*) > 0 THEN 'USER_HAS_VOTED_BEFORE'
					ELSE 'BALLOT_OK'
				END as ballot_status
			FROM ballot
			WHERE poll_id = $1 AND represented_meeting_user_id = $5
		),
		inserted AS (
			INSERT INTO ballot
			(poll_id, value, weight, acting_meeting_user_id, represented_meeting_user_id)
			SELECT $1, $2, $3, $4, $5
			FROM poll_check p, ballot_check b
			WHERE p.poll_status = 'POLL_VALID' AND b.ballot_status = 'BALLOT_OK'
			RETURNING id
		)
		SELECT
			CASE
				WHEN i.id IS NOT NULL THEN 'VALID'
				WHEN p.poll_status != 'POLL_VALID' THEN p.poll_status
				WHEN b.ballot_status != 'BALLOT_OK' THEN b.ballot_status
				ELSE 'UNKNOWN_ERROR'
			END as status
		FROM poll_check p, ballot_check b
		LEFT JOIN inserted i ON true;`

	var status string
	err = v.querier.QueryRow(ctx, sql, pollID, ballotValue, weight, actingMeetingUserID, representedMeetingUserID).Scan(
		&status,
	)
	if err != nil {
		return fmt.Errorf("insert ballot: %w", err)
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

// encryptBallot encrypts the given value with AES using the key for secret polls.
func (v *Vote) encryptBallot(ballotValue string) (string, error) {
	nonce := make([]byte, v.gcmForSecretPolls.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("create nonce: %w", err)
	}

	encryptedValue := v.gcmForSecretPolls.Seal(nonce, nonce, []byte(ballotValue), nil)

	return base64.StdEncoding.EncodeToString(encryptedValue), nil
}

// allowedToVote checks, that the represented user can vote and the acting user
// can vote for him.
func allowedToVote(
	ctx context.Context,
	ds *dsfetch.Fetch,
	poll dsmodels.Poll,
	representedMeetingUserID int,
	actingMeetingUserID int,
) error {
	if representedMeetingUserID == 0 {
		return MessageError(ErrNotAllowed, "You can not vote for anonymous.")
	}

	if err := ensurePresent(ctx, ds, actingMeetingUserID); err != nil {
		return fmt.Errorf("ensure acting user %d is present: %w", actingMeetingUserID, err)
	}

	groupIDs, err := ds.MeetingUser_GroupIDs(representedMeetingUserID).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetching groups of meeting_user %d: %w", representedMeetingUserID, err)
	}

	if !hasCommon(groupIDs, poll.EntitledGroupIDs) {
		return MessageErrorf(ErrNotAllowed, "Meeting User %d is not allowed to vote. He is not in an entitled group", representedMeetingUserID)
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

	if delegationActivated && forbitDelegateToVote && !delegation.Null() && representedMeetingUserID == actingMeetingUserID {
		return MessageError(ErrNotAllowed, "You have delegated your vote and therefore can not vote for your self")
	}

	if representedMeetingUserID == actingMeetingUserID {
		return nil
	}

	if !delegationActivated {
		return MessageErrorf(ErrNotAllowed, "Vote delegation is not activated in meeting %d", poll.MeetingID)
	}

	if id, ok := delegation.Value(); !ok || id != actingMeetingUserID {
		return MessageErrorf(ErrNotAllowed, "You can not vote for meeting user %d", representedMeetingUserID)
	}

	return nil
}

// CalcVoteWeight calculates the vote weight for a user in a meeting.
//
// voteweight is a DecimalField with 6 zeros.
func CalcVoteWeight(ctx context.Context, fetch *dsfetch.Fetch, meetingUserID int) (decimal.Decimal, error) {
	defaultVoteWeight, _ := decimal.NewFromString("1.000000")
	userID, err := fetch.MeetingUser_UserID(meetingUserID).Value(ctx)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("getting user ID from meeting user: %w", err)
	}

	meetingID, err := fetch.MeetingUser_MeetingID(meetingUserID).Value(ctx)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("getting meeting ID from meeting user: %w", err)
	}

	var voteWeightEnabled bool
	var meetingUserVoteWeight decimal.Decimal
	var userDefaultVoteWeight decimal.Decimal
	fetch.Meeting_UsersEnableVoteWeight(meetingID).Lazy(&voteWeightEnabled)
	fetch.MeetingUser_VoteWeight(meetingUserID).Lazy(&meetingUserVoteWeight)
	fetch.User_DefaultVoteWeight(userID).Lazy(&userDefaultVoteWeight)

	if err := fetch.Execute(ctx); err != nil {
		return decimal.Decimal{}, fmt.Errorf("getting vote weight values from db: %w", err)
	}

	if !voteWeightEnabled {
		return defaultVoteWeight, nil
	}

	if !meetingUserVoteWeight.IsZero() {
		return meetingUserVoteWeight, nil
	}

	if !userDefaultVoteWeight.IsZero() {
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

func ensurePresent(ctx context.Context, ds *dsfetch.Fetch, meetingUser int) error {
	meetingID, err := ds.MeetingUser_MeetingID(meetingUser).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetching meeting ID: %w", err)
	}

	userID, err := ds.MeetingUser_UserID(meetingUser).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetching user ID: %w", err)
	}

	presentMeetings, err := ds.User_IsPresentInMeetingIDs(userID).Value(ctx)
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
