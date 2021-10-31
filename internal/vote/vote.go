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

func (v *Vote) backend(p pollConfig) Backend {
	backend := v.longBackend
	if p.backend == "fast" {
		backend = v.fastBackend
	}
	log.Debug("Used backend: %v", backend)
	return backend
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

	recorder := datastore.NewRecorder(v.ds)
	ds := datastore.NewRequest(recorder)

	poll, err := loadPoll(ctx, ds, pollID)
	if err != nil {
		return fmt.Errorf("loading poll: %w", err)
	}

	if poll.pollType == "analog" {
		return MessageError{ErrInvalid, "Analog poll can not be created"}
	}

	if poll.state != "started" {
		return MessageError{ErrInternal, fmt.Sprintf("Poll state is %s, only started polls can be created", poll.state)}
	}

	if err := poll.preload(ctx, ds); err != nil {
		return fmt.Errorf("preloading data: %w", err)
	}
	log.Debug("Preload cache. Received keys: %v", recorder.Keys())

	backend := v.backend(poll)
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

	ds := datastore.NewRequest(v.ds)
	poll, err := loadPoll(ctx, ds, pollID)
	if err != nil {
		return fmt.Errorf("loading poll: %w", err)
	}

	backend := v.backend(poll)
	objects, userIDs, err := backend.Stop(ctx, pollID)
	if err != nil {
		var errNotExist interface{ DoesNotExist() }
		if errors.As(err, &errNotExist) {
			return MessageError{ErrNotExists, fmt.Sprintf("Poll %d does not exist in the backend", pollID)}
		}

		return fmt.Errorf("fetching vote objects: %w", err)
	}

	// Convert vote objects to json.RawMessage
	encodableObjects := make([]json.RawMessage, len(objects))
	for i := range objects {
		encodableObjects[i] = objects[i]
	}

	if userIDs == nil {
		userIDs = []int{}
	}

	out := struct {
		Votes []json.RawMessage `json:"votes"`
		Users []int             `json:"user_ids"`
	}{
		encodableObjects,
		userIDs,
	}

	if err := json.NewEncoder(w).Encode(out); err != nil {
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
		return fmt.Errorf("clearing fastBackend: %w", err)
	}

	if err := v.longBackend.Clear(ctx, pollID); err != nil {
		return fmt.Errorf("clearing longBackend: %w", err)
	}
	return nil
}

// ClearAll removes all knowlage of all polls and the datastore-cache.
func (v *Vote) ClearAll(ctx context.Context) (err error) {
	log.Debug("Receive clearAll event")
	defer func() {
		log.Debug("End clearAll event with error: %v", err)
	}()

	// Reset the cache if it has the ResetCach() method.
	type ResetCacher interface {
		ResetCache()
	}
	if r, ok := v.ds.(ResetCacher); ok {
		r.ResetCache()
	}

	if err := v.fastBackend.ClearAll(ctx); err != nil {
		return fmt.Errorf("clearing fastBackend: %w", err)
	}

	if err := v.longBackend.ClearAll(ctx); err != nil {
		return fmt.Errorf("clearing long Backend: %w", err)
	}
	return nil
}

// Vote validates and saves the vote.
func (v *Vote) Vote(ctx context.Context, pollID, requestUser int, r io.Reader) (err error) {
	log.Debug("Receive vote event for poll %d from user %d", pollID, requestUser)
	defer func() {
		log.Debug("End vote event with error: %v", err)
	}()

	ds := datastore.NewRequest(v.ds)
	poll, err := loadPoll(ctx, ds, pollID)
	if err != nil {
		return fmt.Errorf("loading poll: %w", err)
	}
	log.Debug("Poll config: %v", poll)

	presentMeetings, err := ds.User_IsPresentInMeetingIDs(requestUser).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetching is present in meetings: %w", err)
	}

	if !isPresent(poll.meetingID, presentMeetings) {
		return MessageError{ErrNotAllowed, fmt.Sprintf("You have to be present in meeting %d", poll.meetingID)}
	}

	var vote ballot
	if err := json.NewDecoder(r).Decode(&vote); err != nil {
		return MessageError{ErrInvalid, fmt.Sprintf("decoding payload: %v", err)}
	}
	if vote.UserID == 0 {
		vote.UserID = requestUser
	}
	log.Debug("Ballot: %v", vote)

	if err := vote.validate(poll); err != nil {
		return fmt.Errorf("validating vote: %w", err)
	}

	backend := v.backend(poll)

	if vote.UserID != requestUser {
		delegation, err := ds.User_VoteDelegatedToID(vote.UserID, poll.meetingID).Value(ctx)
		if err != nil {
			// If the user from the request body does not exist, then delegation
			// will be 0. This case is handled below.
			var errDoesNotExist datastore.DoesNotExistError
			if !errors.As(err, &errDoesNotExist) {
				return fmt.Errorf("fetching delegation from user %d in meeting %d: %w", vote.UserID, poll.meetingID, err)
			}
		}

		if delegation != requestUser {
			return MessageError{ErrNotAllowed, fmt.Sprintf("You can not vote for user %d", vote.UserID)}
		}
		log.Debug("User %d is voting for user %d", requestUser, vote.UserID)
	}

	groupIDs, err := ds.User_GroupIDs(vote.UserID, poll.meetingID).Value(ctx)
	if err := ds.Err(); err != nil {
		return fmt.Errorf("fetching groups of user %d in meeting %d: %w", vote.UserID, poll.meetingID, err)
	}

	if !equalElement(groupIDs, poll.groups) {
		return MessageError{ErrNotAllowed, fmt.Sprintf("User %d is not allowed to vote", vote.UserID)}
	}

	// voteData.Weight is a DecimalField with 6 zeros.
	var voteWeight string
	if ds.Meeting_UsersEnableVoteWeight(poll.meetingID).ErrorLater(ctx) {
		voteWeight = ds.User_VoteWeight(vote.UserID, poll.meetingID).ErrorLater(ctx)
		if voteWeight == "" {
			voteWeight = ds.User_DefaultVoteWeight(vote.UserID).ErrorLater(ctx)
		}
	}
	if err := ds.Err(); err != nil {
		return fmt.Errorf("getting vote weight: %w", err)
	}

	if voteWeight == "" {
		voteWeight = "1.000000"
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

	if poll.pollType == "pseudoanonymous" {
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

	// Stop ends a poll and returns all poll objects and all userIDs from users
	// that have voted. It is ok to call Stop() on a stopped poll. On a unknown
	// poll `DoesNotExist()` has to be returned.
	Stop(ctx context.Context, pollID int) ([][]byte, []int, error)

	// Clear has to remove all data. It can be called on a started or stopped or
	// non existing poll.
	Clear(ctx context.Context, pollID int) error

	// ClearAll removes all data from the backend.
	ClearAll(ctx context.Context) error

	fmt.Stringer
}

type pollConfig struct {
	id            int
	meetingID     int
	backend       string
	pollType      string
	method        string
	groups        []int
	globalYes     bool
	globalNo      bool
	globalAbstain bool
	minAmount     int
	maxAmount     int
	options       []int
	state         string
}

func loadPoll(ctx context.Context, ds *datastore.Request, pollID int) (pollConfig, error) {
	p := pollConfig{id: pollID}
	ds.Poll_MeetingID(pollID).Lazy(&p.meetingID)
	ds.Poll_Backend(pollID).Lazy(&p.backend)
	ds.Poll_Type(pollID).Lazy(&p.pollType)
	ds.Poll_Pollmethod(pollID).Lazy(&p.method)
	ds.Poll_EntitledGroupIDs(pollID).Lazy(&p.groups)
	ds.Poll_GlobalYes(pollID).Lazy(&p.globalYes)
	ds.Poll_GlobalNo(pollID).Lazy(&p.globalNo)
	ds.Poll_GlobalAbstain(pollID).Lazy(&p.globalAbstain)
	ds.Poll_MinVotesAmount(pollID).Lazy(&p.minAmount)
	ds.Poll_MaxVotesAmount(pollID).Lazy(&p.maxAmount)
	ds.Poll_OptionIDs(pollID).Lazy(&p.options)
	ds.Poll_State(pollID).Lazy(&p.state)

	if err := ds.Execute(ctx); err != nil {
		return pollConfig{}, fmt.Errorf("loading polldata from datastore: %w", err)
	}

	return p, nil
}

// preload loads all data in the cache, that is needed later for the vote
// requests.
func (p pollConfig) preload(ctx context.Context, ds *datastore.Request) error {
	// TODO: Test that this uses less requests.
	for _, groupID := range p.groups {
		userIDs, err := ds.Group_UserIDs(groupID).Value(ctx)
		if err != nil {
			return fmt.Errorf("loading users of group %q: %w", groupID, err)
		}
		for _, userID := range userIDs {
			ds.User_GroupIDs(userID, p.meetingID)
			ds.User_VoteWeight(userID, p.meetingID)
			ds.User_DefaultVoteWeight(userID)
			delegatedUserID := ds.User_VoteDelegatedToID(userID, p.meetingID).ErrorLater(ctx)
			if delegatedUserID == 0 {
				delegatedUserID = userID
			}
			ds.User_IsPresentInMeetingIDs(delegatedUserID)

		}
	}
	ds.Meeting_UsersEnableVoteWeight(p.meetingID)

	if err := ds.Execute(ctx); err != nil {
		return fmt.Errorf("preloading present users: %w", err)
	}
	return nil
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
	if poll.minAmount == 0 {
		poll.minAmount = 1
	}

	if poll.maxAmount == 0 {
		poll.maxAmount = 1
	}

	allowedOptions := make(map[int]bool, len(poll.options))
	for _, o := range poll.options {
		allowedOptions[o] = true
	}

	allowedGlobal := map[string]bool{
		"Y": poll.globalYes,
		"N": poll.globalNo,
		"A": poll.globalAbstain,
	}

	// Helper "error" that is not an error. Should help readability.
	var voteIsValid error

	switch poll.method {
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

			if sumAmount < poll.minAmount || sumAmount > poll.maxAmount {
				return InvalidVote("The sum of your answers has to be between %d and %d", poll.minAmount, poll.maxAmount)
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

				if yna != "Y" && yna != "N" && (yna != "A" || poll.method != "YNA") {
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

// equalElement returns true, if g1 and g2 have at lease one equal element.
func equalElement(g1, g2 []int) bool {
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
