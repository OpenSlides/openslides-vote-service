package vote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

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
		return MessageErrorf(ErrInvalid, "Poll %d has not started yet.", pollID)
	}

	tx, err := v.querier.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if poll.State == `started` {
		ds := dsmodels.New(v.flow)

		ballots, err := ds.Ballot(poll.BallotIDs...).Get(ctx)
		if err != nil {
			return fmt.Errorf("fetch votes of poll %d: %w", poll.ID, err)
		}

		if poll.Visibility == "secret" {
			for i := range ballots {
				ballots[i].Value, err = v.decryptBallot(ballots[i].Value)
				if err != nil {
					return fmt.Errorf("decrypting ballot: %w", err)
				}
			}

			// Change the order of the ballots so the new values can not be guessed.
			sort.Slice(ballots, func(i, j int) bool {
				return ballots[i].Value < ballots[j].Value
			})

			// Delete and reinsert old ballots.
			_, err = tx.Exec(ctx, "DELETE FROM ballot_t WHERE poll_id = $1", poll.ID)
			if err != nil {
				return fmt.Errorf("deleting old ballots: %w", err)
			}

			_, err = tx.CopyFrom(
				ctx,
				pgx.Identifier{"ballot_t"},
				[]string{"weight", "split", "value", "poll_id"},
				pgx.CopyFromSlice(len(ballots), func(i int) ([]any, error) {
					return []any{
						ballots[i].Weight,
						ballots[i].Split,
						ballots[i].Value,
						poll.ID,
					}, nil
				}),
			)
			if err != nil {
				return fmt.Errorf("bulk inserting anonymized ballots: %w", err)
			}
		}

		config, err := v.EncodeConfig(ctx, poll)
		if err != nil {
			return fmt.Errorf("encode config: %w", err)
		}

		result, err := CreateResult(pollMethod(poll), config, poll.AllowVoteSplit, ballots)
		if err != nil {
			return fmt.Errorf("create poll result: %w", err)
		}

		votedMeetingUserIDs := make([]int, len(ballots))
		for i, vote := range ballots {
			meetingUserID, set := vote.RepresentedMeetingUserID.Value()
			if !set {
				return fmt.Errorf("vote %d has no representedMeetingUserID", vote.ID)
			}
			votedMeetingUserIDs[i] = meetingUserID
		}

		sql := `UPDATE poll SET result = $1 WHERE id = $2;`
		if _, err := tx.Exec(ctx, sql, result, pollID); err != nil {
			return fmt.Errorf("set result of poll %d: %w", pollID, err)
		}

		if len(votedMeetingUserIDs) > 0 {
			placeholders := make([]string, len(votedMeetingUserIDs))
			args := make([]any, len(votedMeetingUserIDs)*2)

			for i, votedUserID := range votedMeetingUserIDs {
				placeholders[i] = fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2)
				args[i*2] = votedUserID
				args[i*2+1] = pollID
			}

			votedSQL := fmt.Sprintf(
				"INSERT INTO nm_meeting_user_poll_voted_ids_poll_t (meeting_user_id, poll_id) VALUES %s",
				strings.Join(placeholders, ", "),
			)

			if _, err := tx.Exec(ctx, votedSQL, args...); err != nil {
				return fmt.Errorf("insert voted_user_ids to meeting_user relations: %w", err)
			}
		}
	}

	sql := `UPDATE poll SET state = 'finished', published = $1 WHERE id = $2;`
	if _, err := tx.Exec(ctx, sql, publish, pollID); err != nil {
		return fmt.Errorf("set poll %d to finished and publish to %v: %w", pollID, publish, err)
	}

	if anonymize {
		if poll.Visibility == "named" {
			return MessageError(ErrNotAllowed, "A named-poll can not be anonymized.")
		}

		sql := `UPDATE ballot
				SET acting_meeting_user_id = NULL, represented_meeting_user_id = NULL
				WHERE poll_id = $1`

		if _, err := tx.Exec(ctx, sql, pollID); err != nil {
			return fmt.Errorf("anonymize ballots: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// decryptBallot decrypt the given value with AES using the key for secret polls.
func (v *Vote) decryptBallot(encryptedBallot string) (string, error) {
	encryptedValue, err := base64.StdEncoding.DecodeString(encryptedBallot)
	if err != nil {
		return "", fmt.Errorf("base64 decode encrypted ballot: %w", err)
	}

	nonceSize := v.gcmForSecretPolls.NonceSize()
	if len(encryptedValue) < nonceSize {
		return "", fmt.Errorf("encrypted ballot too short")
	}

	nonce, ciphertext := encryptedValue[:nonceSize], encryptedValue[nonceSize:]

	plaintext, err := v.gcmForSecretPolls.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}

	return string(plaintext), nil
}

// CreateResult creates the result from a list of votes.
func CreateResult(method string, config string, allowVoteSplit bool, ballots []dsmodels.Ballot) (string, error) {
	if allowVoteSplit {
		ballots = splitVote(method, config, ballots)
	}

	switch method {
	case methodApproval{}.Name():
		return methodApproval{}.Result(config, ballots)
	case methodSelection{}.Name():
		return methodSelection{}.Result(config, ballots)
	case methodRatingScore{}.Name():
		return methodRatingScore{}.Result(config, ballots)
	case methodRatingApproval{}.Name():
		return methodRatingApproval{}.Result(config, ballots)
	default:
		return "", fmt.Errorf("unknown poll method: %s", method)
	}
}

func splitVote(method string, config string, ballots []dsmodels.Ballot) []dsmodels.Ballot {
	var splittedBallots []dsmodels.Ballot
	for _, ballot := range ballots {
		if !ballot.Split {
			splittedBallots = append(splittedBallots, ballot)
			continue
		}

		splitted, err := split(ballot.Weight, json.RawMessage(ballot.Value))
		if err != nil {
			// If the ballot value can not be splitted, just use it as value.
			// It will probably be counted as invalid.
			splittedBallots = append(splittedBallots, ballot)
			continue
		}

		splittedBallots = append(splittedBallots, ballotsFromSplitted(method, config, ballot, splitted)...)
	}
	return splittedBallots
}

// split split sa vote and valides the weight
func split(maxWeight decimal.Decimal, value json.RawMessage) (map[decimal.Decimal]json.RawMessage, error) {
	var splitVotes map[decimal.Decimal]json.RawMessage
	if err := json.Unmarshal(value, &splitVotes); err != nil {
		return nil, errors.Join(MessageError(ErrInvalid, "Invalid split votes"), err)
	}

	var splitWeightSum decimal.Decimal
	for splitWeight := range splitVotes {
		splitWeightSum = splitWeightSum.Add(splitWeight)
	}

	if splitWeightSum.Cmp(maxWeight) == 1 {
		return nil, MessageError(ErrInvalid, "Split weight exceeds your vote weight.")
	}

	return splitVotes, nil
}
