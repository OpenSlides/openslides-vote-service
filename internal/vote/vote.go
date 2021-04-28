package vote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/OpenSlides/openslides-autoupdate-service/pkg/datastore"
	"github.com/OpenSlides/openslides-vote-service/internal/log"
)

// Vote holds the state of the service.
//
// Vote has to be initializes with vote.New().
type Vote struct {
	fastBackend Backend
	longBackend Backend
	ds          datastore.Getter
}

// New creates an initializes vote service.
func New(fast, long Backend, ds datastore.Getter) *Vote {
	return &Vote{
		fastBackend: fast,
		longBackend: long,
		ds:          ds,
	}
}

// Create an electronic vote.
//
// This function is idempotence. If you call it with the same input, you will
// get the same output. This means, that when a poll is stopped, Create() will
// not throw an error.
func (v *Vote) Create(ctx context.Context, pollID int) (err error) {
	log.Debug("Receive create event for poll %d", pollID)
	defer func() {
		log.Debug("End create event with error: %v", err)
	}()

	fetcher := datastore.NewFetcher(v.ds)
	var config pollConfig
	fetcher.Object(ctx, &config, "poll/%d", pollID)
	for _, gid := range config.Groups {
		for _, id := range fetcher.Ints(ctx, "group/%d/user_ids", gid) {
			fetcher.Ints(ctx, "user/%d/is_present_in_meeting_ids", id)
		}
	}

	log.Debug("Preload cache. Received keys: %v", fetcher.Keys())

	if err := fetcher.Error(); err != nil {
		return fmt.Errorf("fetching poll data: %w", err)
	}

	backend := v.longBackend
	if config.Backend == "fast" {
		backend = v.fastBackend
	}
	log.Debug("Used backend: %v", backend)

	if err := backend.Start(ctx, pollID); err != nil {
		return fmt.Errorf("starting poll in the backend: %w", err)
	}

	return nil
}

// Stop ends a poll.
//
// This method is idempotence. Many requests with the same pollID will return
// the same data. Calling vote.Clear will stop this behavior.
func (v *Vote) Stop(ctx context.Context, pollID int, w io.Writer) (err error) {
	log.Debug("Receive stop event for poll %d", pollID)
	defer func() {
		log.Debug("End stop event with error: %v", err)
	}()

	fetcher := datastore.NewFetcher(v.ds)
	var config pollConfig
	fetcher.Object(ctx, &config, "poll/%d", pollID)
	if err := fetcher.Error(); err != nil {
		return fmt.Errorf("fetching poll data: %w", err)
	}

	if config.ID == 0 {
		return fmt.Errorf("Poll %d does not exist in the datastore", pollID)
	}

	backend := v.longBackend
	if config.Backend == "fast" {
		backend = v.fastBackend
	}
	log.Debug("Used backend: %v", backend)

	objects, err := backend.Stop(ctx, pollID)
	if err != nil {
		var errNotExist interface{ DoesNotExist() }
		if errors.As(err, &errNotExist) {
			return MessageError{ErrNotExists, fmt.Sprintf("Poll %d does not exist in the datastore", pollID)}
		}

		return fmt.Errorf("fetching vote objects: %w", err)
	}

	encodableObjects := make([]json.RawMessage, len(objects))
	for i := range objects {
		encodableObjects[i] = objects[i]
	}

	if err := json.NewEncoder(w).Encode(encodableObjects); err != nil {
		return fmt.Errorf("encoding and sending objects: %w", err)
	}

	return nil
}

// Clear removes all knowlage of a poll.
func (v *Vote) Clear(ctx context.Context, pollID int) (err error) {
	log.Debug("Receive clear event for poll %d", pollID)
	defer func() {
		log.Debug("End clear event with error: %v", err)
	}()

	if err := v.fastBackend.Clear(ctx, pollID); err != nil {
		return fmt.Errorf("clearing the config: %w", err)
	}

	if err := v.longBackend.Clear(ctx, pollID); err != nil {
		return fmt.Errorf("clearing the config: %w", err)
	}
	return nil
}

// Vote validates and saves the vote.
func (v *Vote) Vote(ctx context.Context, pollID, requestUser int, r io.Reader) (err error) {
	log.Debug("Receive vote event for poll %d from user %d", pollID, requestUser)
	defer func() {
		log.Debug("End vote event with error: %v", err)
	}()

	fetcher := datastore.NewFetcher(v.ds)
	var poll pollConfig
	fetcher.Object(ctx, &poll, "poll/%d", pollID)
	presentMeetings := fetcher.Ints(ctx, "user/%d/is_present_in_meeting_ids", requestUser)
	if err := fetcher.Error(); err != nil {
		return fmt.Errorf("fetching poll data: %w", err)
	}
	log.Debug("Poll config: %v", poll)

	if !isPresent(poll.MeetingID, presentMeetings) {
		return MessageError{ErrNotAllowed, fmt.Sprintf("You have to be present in meeting %d", poll.MeetingID)}
	}

	var vote ballot
	if err := json.NewDecoder(r).Decode(&vote); err != nil {
		return MessageError{ErrInvalid, fmt.Sprintf("invalid json: %v", err)}
	}
	if vote.UserID == 0 {
		vote.UserID = requestUser
	}
	log.Debug("Ballot: %v", vote)

	if err := vote.validate(poll); err != nil {
		return fmt.Errorf("validating vote: %w", err)
	}

	backend := v.longBackend
	if poll.Backend == "fast" {
		backend = v.fastBackend
	}
	log.Debug("Used backend: %v", backend)

	if vote.UserID != requestUser {
		delegation := fetcher.Int(ctx, "user/%d/vote_delegated_$%d_to_id", vote.UserID, poll.MeetingID)
		if err := fetcher.Error(); err != nil {
			var errNotExist datastore.DoesNotExistError
			if !errors.As(err, &errNotExist) {
				return fmt.Errorf("fetching delegation from user %d in meeting %d: %v", vote.UserID, poll.MeetingID, err)
			}
		}

		if delegation != requestUser {
			return MessageError{ErrNotAllowed, fmt.Sprintf("You can not vote for user %d", vote.UserID)}
		}
		log.Debug("User %d is voting for user %d", requestUser, vote.UserID)
	}

	groupIDs := fetcher.Ints(ctx, "user/%d/group_$%d_ids", vote.UserID, poll.MeetingID)
	if err := fetcher.Error(); err != nil {
		var errNotExist datastore.DoesNotExistError
		if !errors.As(err, &errNotExist) {
			return fmt.Errorf("fetching groups of user %d in meeting %d: %v", vote.UserID, poll.MeetingID, err)
		}
	}

	if !sliceMatch(groupIDs, poll.Groups) {
		return MessageError{ErrNotAllowed, fmt.Sprintf("User %d is not allowed to vote", vote.UserID)}
	}

	var voteWeightConfig bool
	fetcher.Value(ctx, &voteWeightConfig, "meeting/%d/users_enable_vote_weight", poll.MeetingID)
	if err := fetcher.Error(); err != nil {
		var errNotExist datastore.DoesNotExistError
		if !errors.As(err, &errNotExist) {
			return fmt.Errorf("fetching users_enable_vote_weight of meeting %d: %v", poll.MeetingID, err)
		}
	}

	// voteData.Weight is a DecimalField with 6 zeros.
	voteWeight := "1.000000"
	if voteWeightConfig {
		voteWeight = fetcher.String(ctx, "user/%d/vote_weight_$%d", vote.UserID, poll.MeetingID)
		if err := fetcher.Error(); err != nil {
			var errNotExist datastore.DoesNotExistError
			if !errors.As(err, &errNotExist) {
				return fmt.Errorf("fetching vote weight of user %d in meeting %d: %v", vote.UserID, poll.MeetingID, err)
			}
			voteWeight = "1.000000"
		}
	}
	log.Debug("Using voteWeight %s", voteWeight)

	voteData := struct {
		RequestUser int             `json:"request_user_id,omitempty"`
		VoteUser    int             `json:"vote_user_id,omitempty"`
		Value       json.RawMessage `json:"value"`
		Weight      string          `json:"weight"`
	}{
		requestUser,
		vote.UserID,
		vote.Value.original,
		voteWeight,
	}

	if poll.Type == "pseudoanonymous" {
		voteData.RequestUser = 0
		voteData.VoteUser = 0
	}

	bs, err := json.Marshal(voteData)
	if err != nil {
		return fmt.Errorf("decoding vote data: %w", err)
	}
	log.Debug("Saving vote date: %s", bs)

	if err := backend.Vote(ctx, pollID, vote.UserID, bs); err != nil {
		var errNotExist interface{ DoesNotExist() }
		if errors.As(err, &errNotExist) {
			return ErrNotExists
		}

		var errDoupleVote interface{ DoupleVote() }
		if errors.As(err, &errDoupleVote) {
			return ErrDoubleVote
		}

		var errNotOpen interface{ Stopped() }
		if errors.As(err, &errNotOpen) {
			return ErrStopped
		}

		return fmt.Errorf("save vote: %w", err)
	}

	// TODO: Save vote_count

	return nil
}

// Backend is a storage for the poll options.
type Backend interface {
	// Start opens the poll for votes. To start a poll that is already started
	// is ok. To start an stopped poll is also ok, but it has to be a noop (the
	// stop-state does not change).
	Start(ctx context.Context, pollID int) error

	// Vote saves vote data into the backend. The backend has to check that the
	// poll is started and the userID has not voted before.
	//
	// If the user has already voted, an Error with method `DoupleVote()` has to
	// be returned. If the poll has not started, an error with the method
	// `DoesNotExist()` is required. An a stopped vote, it has to be `Stopped()`.
	Vote(ctx context.Context, pollID int, userID int, object []byte) error

	// Stop ends a poll and returns all poll objects. It is ok the call Stop on
	// a stopped poll. On a unknown poll `DoesNotExist()` has to be returned.
	Stop(ctx context.Context, pollID int) ([][]byte, error)

	// Clear has to remove all data. It can be called on a started or stopped or
	// non existing poll.
	Clear(ctx context.Context, pollID int) error

	fmt.Stringer
}

type pollConfig struct {
	ID            int    `json:"id"`
	MeetingID     int    `json:"meeting_id"`
	Backend       string `json:"backend"`
	Type          string `json:"type"`
	Method        string `json:"pollmethod"`
	Groups        []int  `json:"entitled_group_ids"`
	GlobalYes     bool   `json:"global_yes"`
	GlobalNo      bool   `json:"global_no"`
	GlobalAbstain bool   `json:"global_abstain"`
	MinAmount     int    `json:"min_votes_amount"`
	MaxAmount     int    `json:"max_votes_amount"`
	Options       []int  `json:"option_ids"`
}

func (p pollConfig) String() string {
	bs, err := json.Marshal(p)
	if err != nil {
		return fmt.Sprintf("Error decoding pollConfig: %v", err)
	}
	return string(bs)
}

type ballot struct {
	UserID int         `json:"user_id"`
	Value  ballotValue `json:"value"`
}

func (v ballot) String() string {
	bs, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("Error decoding ballot: %v", err)
	}
	return string(bs)
}

func (v *ballot) validate(poll pollConfig) error {
	// TODO: global options
	if poll.MinAmount == 0 {
		poll.MinAmount = 1
	}

	if poll.MaxAmount == 0 {
		poll.MaxAmount = 1
	}

	allowedOptions := make(map[int]bool, len(poll.Options))
	for _, o := range poll.Options {
		allowedOptions[o] = true
	}

	allowedGlobal := map[string]bool{
		"Y": poll.GlobalYes,
		"N": poll.GlobalNo,
		"A": poll.GlobalAbstain,
	}

	// Helper "error" that is not an error. Should help readability.
	var voteIsValid error

	switch poll.Method {
	case "Y", "N":
		switch v.Value.Type() {
		case ballotValueString:
			// The user answered with Y, N or A (or another invalid string).
			if !allowedGlobal[v.Value.str] {
				return InvalidVote("Global vote %s is not enabled", v.Value.str)
			}
			return voteIsValid

		case ballotValueOptionAmount:
			var sumAmount int
			for optionID, amount := range v.Value.optionAmount {
				if amount < 0 {
					return InvalidVote("Your vote for option %d has to be >= 0", optionID)
				}

				if !allowedOptions[optionID] {
					return InvalidVote("Option_id %d does not belong to the poll", optionID)
				}

				sumAmount += amount
			}

			if sumAmount < poll.MinAmount || sumAmount > poll.MaxAmount {
				return InvalidVote("The sum of your answers has to be between %d and %d", poll.MinAmount, poll.MaxAmount)
			}

			return voteIsValid

		default:
			return MessageError{ErrInvalid, "Your vote has a wrong format"}
		}

	case "YN", "YNA":
		switch v.Value.Type() {
		case ballotValueString:
			// The user answered with Y, N or A (or another invalid string).
			if !allowedGlobal[v.Value.str] {
				return InvalidVote("Global vote %s is not enabled", v.Value.str)
			}
			return voteIsValid

		case ballotValueOptionString:
			for optionID, yna := range v.Value.optionYNA {
				if !allowedOptions[optionID] {
					return InvalidVote("Option_id %d does not belong to the poll", optionID)
				}

				if yna != "Y" && yna != "N" && (yna != "A" || poll.Method != "YNA") {
					// Valid that given data matches poll method.
					return InvalidVote("Data for option %d does not fit the poll method.", optionID)
				}
			}
			return voteIsValid

		default:
			return InvalidVote("Your vote has a wrong format")
		}

	default:
		return InvalidVote("Your vote has a wrong format")
	}
}

// voteData is the data a user sends as his vote.
type ballotValue struct {
	str          string
	optionAmount map[int]int
	optionYNA    map[int]string

	original json.RawMessage
}

func (v ballotValue) MarshalJSON() ([]byte, error) {
	return v.original, nil
}

func (v *ballotValue) UnmarshalJSON(b []byte) error {
	v.original = b

	if err := json.Unmarshal(b, &v.str); err == nil {
		// voteData is a string
		return nil
	}

	if err := json.Unmarshal(b, &v.optionAmount); err == nil {
		// voteData is option_id to amount
		return nil
	}
	v.optionAmount = nil

	if err := json.Unmarshal(b, &v.optionYNA); err == nil {
		// voteData is option_id to string
		return nil
	}

	return fmt.Errorf("unknown vote value: `%s`", b)
}

const (
	ballotValueUnknown = iota
	ballotValueString
	ballotValueOptionAmount
	ballotValueOptionString
)

func (v *ballotValue) Type() int {
	if v.str != "" {
		return ballotValueString
	}

	if v.optionAmount != nil {
		return ballotValueOptionAmount
	}

	if v.optionYNA != nil {
		return ballotValueOptionString
	}

	return ballotValueUnknown
}

func isPresent(meetingID int, presentMeetings []int) bool {
	for _, present := range presentMeetings {
		if present == meetingID {
			return true
		}
	}
	return false
}

// sliceMatch returns true, if g1 and g2 have at lease one same element.
func sliceMatch(g1, g2 []int) bool {
	set := make(map[int]bool, len(g1))
	for _, e := range g1 {
		set[e] = true
	}
	for _, e := range g2 {
		if set[e] {
			return true
		}
	}
	return false
}
