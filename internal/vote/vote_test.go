package vote_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/OpenSlides/openslides-autoupdate-service/pkg/dsmock"
	"github.com/OpenSlides/openslides-vote-service/internal/backends/memory"
	"github.com/OpenSlides/openslides-vote-service/internal/vote"
)

func TestVoteCreate(t *testing.T) {
	t.Run("Not started poll", func(t *testing.T) {
		closed := make(chan struct{})
		defer close(closed)

		backend := memory.New()
		ds := dsmock.NewMockDatastore(closed, dsmock.YAMLData(`
		poll:
			1:
				meeting_id: 5
				state: started

		group/1/user_ids: [1]
		user/1/is_present_in_meeting_ids: [1]
		meeting/5/id: 5
		`))

		v := vote.New(backend, backend, ds, &StubMessageBus{})

		if err := v.Create(context.Background(), 1); err != nil {
			t.Errorf("Create returned unexpected error: %v", err)
		}

		if c := len(ds.Requests()); c > 2 {
			t.Errorf("Create used %d requests to the datastore, expected max 2: %v", c, ds.Requests())
		}

		// After a poll was created, it has to be possible to send votes.
		_, err := backend.Vote(context.Background(), 1, 1, []byte("something"))
		if err != nil {
			t.Errorf("Vote after create retuen and unexpected error: %v", err)
		}
	})

	t.Run("Create poll a second time", func(t *testing.T) {
		backend := memory.New()
		ds := StubGetter{data: dsmock.YAMLData(`
		poll:
			1:
				meeting_id: 5
				type: named
				state: started

		group/1/user_ids: [1]
		user/1/is_present_in_meeting_ids: [1]
		meeting/5/id: 5
		`)}
		v := vote.New(backend, backend, &ds, &StubMessageBus{})
		v.Create(context.Background(), 1)

		if err := v.Create(context.Background(), 1); err != nil {
			t.Errorf("Create returned unexpected error: %v", err)
		}
	})

	t.Run("Create a stopped poll", func(t *testing.T) {
		backend := memory.New()
		ds := StubGetter{data: dsmock.YAMLData(`
		poll:
			1:
				meeting_id: 5
				type: named
				state: started

		group/1/user_ids: [1]
		user/1/is_present_in_meeting_ids: [1]
		meeting/5/id: 5
		`)}
		v := vote.New(backend, backend, &ds, &StubMessageBus{})
		v.Create(context.Background(), 1)

		if _, _, err := backend.Stop(context.Background(), 1); err != nil {
			t.Fatalf("Stop returned unexpected error: %v", err)
		}

		if err := v.Create(context.Background(), 1); err != nil {
			t.Errorf("Create returned unexpected error: %v", err)
		}
	})

	t.Run("Create an anolog poll", func(t *testing.T) {
		backend := memory.New()
		ds := StubGetter{data: dsmock.YAMLData(`
		poll:
			1:
				meeting_id: 5
				type: analog
				state: started

		group/1/user_ids: [1]
		user/1/is_present_in_meeting_ids: [1]
		`)}
		v := vote.New(backend, backend, &ds, &StubMessageBus{})

		err := v.Create(context.Background(), 1)

		if err == nil {
			t.Errorf("Got no error, expected `Some error`")
		}
	})

	t.Run("Create an created poll", func(t *testing.T) {
		backend := memory.New()
		ds := StubGetter{data: dsmock.YAMLData(`
		poll:
			1:
				meeting_id: 5
				type: named
				state: created

		group/1/user_ids: [1]
		user/1/is_present_in_meeting_ids: [1]
		`)}
		v := vote.New(backend, backend, &ds, &StubMessageBus{})

		err := v.Create(context.Background(), 1)

		if err == nil {
			t.Errorf("Got no error, expected `Some error`")
		}
	})

	t.Run("Create an finished poll", func(t *testing.T) {
		backend := memory.New()
		ds := StubGetter{data: dsmock.YAMLData(`
		poll:
			1:
				meeting_id: 5
				type: named
				state: finished

		group/1/user_ids: [1]
		user/1/is_present_in_meeting_ids: [1]
		`)}
		v := vote.New(backend, backend, &ds, &StubMessageBus{})

		err := v.Create(context.Background(), 1)

		if err == nil {
			t.Errorf("Got no error, expected `Some error`")
		}
	})

	t.Run("Create an finished poll", func(t *testing.T) {
		backend := memory.New()
		ds := StubGetter{data: dsmock.YAMLData(`
		poll:
			1:
				meeting_id: 5
				type: named
				state: published

		group/1/user_ids: [1]
		user/1/is_present_in_meeting_ids: [1]
		`)}
		v := vote.New(backend, backend, &ds, &StubMessageBus{})

		err := v.Create(context.Background(), 1)

		if err == nil {
			t.Errorf("Got no error, expected `Some error`")
		}
	})
}

func TestVoteCreatePreloadData(t *testing.T) {
	closed := make(chan struct{})
	defer close(closed)

	backend := memory.New()
	ds := dsmock.NewMockDatastore(closed, dsmock.YAMLData(`
	poll/1:
		meeting_id: 5
		entitled_group_ids: [1]
		state: started
	
	group:
		1:
			user_ids: [1,2]
	user:
		1:
			is_present_in_meeting_ids: [1]
		2:
			is_present_in_meeting_ids: [1]
	meeting/5/id: 5
	`))
	v := vote.New(backend, backend, ds, &StubMessageBus{})

	if err := v.Create(context.Background(), 1); err != nil {
		t.Errorf("Create returned unexpected error: %v", err)
	}

	if !ds.KeysRequested("poll/1/meeting_id", "user/1/is_present_in_meeting_ids", "user/2/is_present_in_meeting_ids") {
		t.Fatalf("Not all keys where preloaded.")
	}
}

func TestVoteCreateDSError(t *testing.T) {
	backend := memory.New()
	ds := StubGetter{err: errors.New("Some error")}
	v := vote.New(backend, backend, &ds, &StubMessageBus{})
	err := v.Create(context.Background(), 1)

	if err == nil {
		t.Errorf("Got no error, expected `Some error`")
	}
}

func TestVoteStop(t *testing.T) {
	backend := memory.New()
	v := vote.New(backend, backend, &StubGetter{data: dsmock.YAMLData(`
	poll/1/meeting_id: 1
	poll/2/meeting_id: 1
	poll/3/meeting_id: 1
	`)}, &StubMessageBus{})

	t.Run("Unknown poll", func(t *testing.T) {
		buf := new(bytes.Buffer)
		err := v.Stop(context.Background(), 1, buf)
		if !errors.Is(err, vote.ErrNotExists) {
			t.Errorf("Stopping an unknown poll has to return an ErrNotExists, got: %v", err)
		}
	})

	t.Run("Known poll", func(t *testing.T) {
		if err := backend.Start(context.Background(), 2); err != nil {
			t.Fatalf("Start returned an unexpected error: %v", err)
		}

		backend.Vote(context.Background(), 2, 1, []byte(`"polldata1"`))
		backend.Vote(context.Background(), 2, 2, []byte(`"polldata2"`))

		buf := new(bytes.Buffer)
		if err := v.Stop(context.Background(), 2, buf); err != nil {
			t.Fatalf("Stop returned unexpected error: %v", err)
		}

		expect := `{"votes":["polldata1","polldata2"],"user_ids":[1,2]}`
		if got := strings.TrimSpace(buf.String()); got != expect {
			t.Errorf("Stop wrote `%s`, expected `%s`", got, expect)
		}

		_, err := backend.Vote(context.Background(), 2, 3, []byte(`"polldata3"`))
		var errStopped interface{ Stopped() }
		if !errors.As(err, &errStopped) {
			t.Errorf("Stop did not stop the poll in the backend.")
		}
	})

	t.Run("Poll without data", func(t *testing.T) {
		if err := backend.Start(context.Background(), 3); err != nil {
			t.Fatalf("Start returned an unexpected error: %v", err)
		}

		buf := new(bytes.Buffer)
		if err := v.Stop(context.Background(), 3, buf); err != nil {
			t.Fatalf("Stop returned unexpected error: %v", err)
		}

		expect := `{"votes":[],"user_ids":[]}`
		if got := strings.TrimSpace(buf.String()); got != expect {
			t.Errorf("Stop wrote `%s`, expected `%s`", got, expect)
		}
	})
}

func TestVoteClear(t *testing.T) {
	backend := memory.New()
	v := vote.New(backend, backend, &StubGetter{}, &StubMessageBus{})

	if err := v.Clear(context.Background(), 1); err != nil {
		t.Fatalf("Clear returned unexpected error: %v", err)
	}
}

func TestVoteClearAll(t *testing.T) {
	backend := memory.New()
	v := vote.New(backend, backend, &StubGetter{}, &StubMessageBus{})

	if err := v.ClearAll(context.Background()); err != nil {
		t.Fatalf("ClearAll returned unexpected error: %v", err)
	}
}

func TestVoteVote(t *testing.T) {
	backend := memory.New()
	v := vote.New(backend, backend, &StubGetter{
		data: dsmock.YAMLData(`
		poll/1:
			meeting_id: 1
			entitled_group_ids: [1]
			pollmethod: Y
			global_yes: true
		
		meeting/1/id: 1

		user/1:
			is_present_in_meeting_ids: [1]
			group_$1_ids: [1]
		`),
	}, &StubMessageBus{})

	t.Run("Unknown poll", func(t *testing.T) {
		err := v.Vote(context.Background(), 1, 1, strings.NewReader(`{"value":"Y"}`))

		if !errors.Is(err, vote.ErrNotExists) {
			t.Errorf("Expected ErrNotExists, got: %v", err)
		}
	})

	if err := backend.Start(context.Background(), 1); err != nil {
		t.Fatalf("Starting poll returned unexpected error: %v", err)
	}

	t.Run("Invalid json", func(t *testing.T) {
		err := v.Vote(context.Background(), 1, 1, strings.NewReader(`{123`))

		var errTyped vote.TypeError
		if !errors.As(err, &errTyped) {
			t.Fatalf("Vote() did not return an TypeError, got: %v", err)
		}

		if errTyped != vote.ErrInvalid {
			t.Errorf("Got error type `%s`, expected `%s`", errTyped.Type(), vote.ErrInvalid.Type())
		}
	})

	t.Run("Invalid format", func(t *testing.T) {
		err := v.Vote(context.Background(), 1, 1, strings.NewReader(`{}`))

		var errTyped vote.TypeError
		if !errors.As(err, &errTyped) {
			t.Fatalf("Vote() did not return an TypeError, got: %v", err)
		}

		if errTyped != vote.ErrInvalid {
			t.Errorf("Got error type `%s`, expected `%s`", errTyped.Type(), vote.ErrInvalid.Type())
		}
	})

	t.Run("Valid data", func(t *testing.T) {
		err := v.Vote(context.Background(), 1, 1, strings.NewReader(`{"value":"Y"}`))
		if err != nil {
			t.Fatalf("Vote returned unexpected error: %v", err)
		}

	})

	t.Run("User has voted", func(t *testing.T) {
		err := v.Vote(context.Background(), 1, 1, strings.NewReader(`{"value":"Y"}`))
		if err == nil {
			t.Fatalf("Vote returned no error")
		}

		var errTyped vote.TypeError
		if !errors.As(err, &errTyped) {
			t.Fatalf("Vote() did not return an TypeError, got: %v", err)
		}

		if errTyped != vote.ErrDoubleVote {
			t.Errorf("Got error type `%s`, expected `%s`", errTyped.Type(), vote.ErrDoubleVote.Type())
		}
	})

	t.Run("Poll is stopped", func(t *testing.T) {
		backend.Stop(context.Background(), 1)

		err := v.Vote(context.Background(), 1, 1, strings.NewReader(`{"value":"Y"}`))
		if err == nil {
			t.Fatalf("Vote returned no error")
		}

		var errTyped vote.TypeError
		if !errors.As(err, &errTyped) {
			t.Fatalf("Vote() did not return an TypeError, got: %v", err)
		}

		if errTyped != vote.ErrStopped {
			t.Errorf("Got error type `%s`, expected `%s`", errTyped.Type(), vote.ErrStopped.Type())
		}
	})
}

func TestVoteMessageBus(t *testing.T) {
	backend := memory.New()
	messageBus := new(StubMessageBus)

	v := vote.New(backend, backend, &StubGetter{
		data: dsmock.YAMLData(`
		poll/1:
			meeting_id: 1
			entitled_group_ids: [1]
			pollmethod: Y
			global_yes: true
			state: started
		
		meeting/1/id: 1

		group/1/id: 1

		user:
			1:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
			2:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
			3:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
			
		`),
	}, messageBus)

	if err := v.Create(context.Background(), 1); err != nil {
		t.Fatalf("Startig poll: %v", err)
	}
	err := v.Vote(context.Background(), 1, 1, strings.NewReader(`{"value":"Y"}`))
	if err != nil {
		t.Fatal(err)
	}
	v.Vote(context.Background(), 1, 2, strings.NewReader(`{"value":"Y"}`))
	v.Vote(context.Background(), 1, 3, strings.NewReader(`{"value":"Y"}`))

	if len(messageBus.messages) != 3 {
		t.Fatalf("Got %d messages, expected 3", len(messageBus.messages))
	}

	for i := 0; i < 3; i++ {
		if messageBus.messages[i] != [2]string{"poll/1/vote_count", fmt.Sprintf("%d", i+1)} {
			t.Errorf("Message %d is %v, expected [poll/1/vote_count %d", i+1, messageBus.messages[i], i+1)
		}
	}

}

func TestVoteNoRequests(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		vote string
	}{
		{
			"normal vote",
			`---
			poll/1:
				meeting_id: 50
				entitled_group_ids: [5]
				pollmethod: Y
				global_yes: true
				state: started
			
			meeting/50/id: 50

			user/1:
				is_present_in_meeting_ids: [50]
				group_$50_ids: [5]

			group/5/user_ids: [1]
			`,
			`{"value":"Y"}`,
		},
		{
			"delegation vote",
			`---
			poll/1:
				meeting_id: 50
				entitled_group_ids: [5]
				pollmethod: Y
				global_yes: true
				state: started
			
			meeting/50/id: 50

			user:
				1:
					is_present_in_meeting_ids: [50]
				2:
					group_$50_ids: [5]
					vote_delegated_$50_to_id: 1

			group/5/user_ids: [2]
			`,
			`{"user_id":2,"value":"Y"}`,
		},
		{
			"vote weight enabled",
			`---
			poll/1:
				meeting_id: 50
				entitled_group_ids: [5]
				pollmethod: Y
				global_yes: true
				state: started
			
			meeting/50/users_enable_vote_weight: true

			user/1:
				is_present_in_meeting_ids: [50]
				group_$50_ids: [5]

			group/5/user_ids: [1]
			`,
			`{"value":"Y"}`,
		},
		{
			"vote weight enabled and delegated",
			`---
			poll/1:
				meeting_id: 50
				entitled_group_ids: [5]
				pollmethod: Y
				global_yes: true
				state: started
			
			meeting/50/users_enable_vote_weight: true

			user:
				1:
					is_present_in_meeting_ids: [50]
				2:
					group_$50_ids: [5]
					vote_delegated_$50_to_id: 1

			group/5/user_ids: [2]
			`,
			`{"user_id":2,"value":"Y"}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			closed := make(chan struct{})
			defer close(closed)

			ds := dsmock.NewMockDatastore(closed, dsmock.YAMLData(tt.data))
			backend := memory.New()
			v := vote.New(backend, backend, ds, &StubMessageBus{})

			if err := v.Create(context.Background(), 1); err != nil {
				t.Fatalf("Can not start poll: %v", err)
			}

			ds.ResetRequests()

			if err := v.Vote(context.Background(), 1, 1, strings.NewReader(tt.vote)); err != nil {
				t.Errorf("Vote returned unexpected error: %v", err)
			}

			if len(ds.Requests()) != 0 {
				t.Errorf("Vote send %d requests to the datastore: %v", len(ds.Requests()), ds.Requests())
			}
		})
	}
}

func TestVoteDelegationAndGroup(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		vote string

		expectVoted int
	}{
		{
			"Not delegated",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true

			meeting/1/id: 1

			user/1:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
			`,
			`{"value":"Y"}`,

			1,
		},

		{
			"Not delegated not present",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true

			meeting/1/id: 1				

			user/1:
				is_present_in_meeting_ids: []
				group_$1_ids: [1]
			`,
			`{"value":"Y"}`,

			0,
		},

		{
			"Not delegated not in group",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true

			meeting/1/id: 1

			user/1:
				is_present_in_meeting_ids: [1]
				group_$1_ids: []
			`,
			`{"value":"Y"}`,

			0,
		},

		{
			"Vote for self",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true
			
			meeting/1/id: 1

			user/1:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
			`,
			`{"user_id": 1, "value":"Y"}`,

			1,
		},

		{
			"Vote for anonymous",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true
			
			meeting/1/id: 1

			user/1:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
			`,
			`{"user_id": 0, "value":"Y"}`,

			0,
		},

		{
			"Vote for other without delegation",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true

			meeting/1/id: 1

			user/1/is_present_in_meeting_ids: [1]
			user/2/group_$1_ids: [1]
			`,
			`{"user_id": 2, "value":"Y"}`,

			0,
		},

		{
			"Vote for other with delegation",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true

			meeting/1/id: 1

			user/1/is_present_in_meeting_ids: [1]
			user/2:
				vote_delegated_$1_to_id: 1
				group_$1_ids: [1]
			`,
			`{"user_id": 2, "value":"Y"}`,

			2,
		},

		{
			"Vote for other with delegation not in group",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true
			
			meeting/1/id: 1

			user/1/is_present_in_meeting_ids: [1]
			user/2:
				vote_delegated_$1_to_id: 1
				group_$1_ids: []
			`,
			`{"user_id": 2, "value":"Y"}`,

			0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			backend := memory.New()
			v := vote.New(backend, backend, &StubGetter{data: dsmock.YAMLData(tt.data)}, &StubMessageBus{})
			backend.Start(context.Background(), 1)

			err := v.Vote(context.Background(), 1, 1, strings.NewReader(tt.vote))

			if tt.expectVoted != 0 {
				if err != nil {
					t.Fatalf("Vote returned unexpected error: %v", err)
				}

				backend.AssertUserHasVoted(t, 1, tt.expectVoted)
				return
			}

			if !errors.Is(err, vote.ErrNotAllowed) {
				t.Fatalf("Expected NotAllowedError, got: %v", err)
			}
		})
	}
}

func TestVoteWeight(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string

		expectWeight string
	}{
		{
			"No weight",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true

			meeting/1/id: 1

			user/1:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
			`,
			"1.000000",
		},
		{
			"Weight enabled, user has no weight",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true

			meeting/1/users_enable_vote_weight: true

			user/1:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
			`,
			"1.000000",
		},
		{
			"Weight enabled, user has default weight",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true

			meeting/1/users_enable_vote_weight: true

			user/1:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
				default_vote_weight: "2.000000"
			`,
			"2.000000",
		},
		{
			"Weight enabled, user has default weight and meeting weight",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true

			meeting/1/users_enable_vote_weight: true

			user/1:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
				default_vote_weight: "2.000000"
				vote_weight_$: [1]
				vote_weight_$1: "3.000000"
			`,
			"3.000000",
		},
		{
			"Weight enabled, user has default weight and meeting weight in other meeting",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				pollmethod: Y
				global_yes: true

			meeting/1/users_enable_vote_weight: true

			user/1:
				is_present_in_meeting_ids: [1]
				group_$1_ids: [1]
				default_vote_weight: "2.000000"
				vote_weight_$: [2]
				vote_weight_$2: "3.000000"
			`,
			"2.000000",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			backend := memory.New()
			v := vote.New(backend, backend, &StubGetter{data: dsmock.YAMLData(tt.data)}, &StubMessageBus{})
			backend.Start(context.Background(), 1)

			if err := v.Vote(context.Background(), 1, 1, strings.NewReader(`{"value":"Y"}`)); err != nil {
				t.Fatalf("vote returned unexpected error: %v", err)
			}

			data, _, _ := backend.Stop(context.Background(), 1)

			if len(data) != 1 {
				t.Fatalf("got %d vote objects, expected one", len(data))
			}

			var decoded struct {
				Weight string `json:"weight"`
			}
			if err := json.Unmarshal(data[0], &decoded); err != nil {
				t.Fatalf("decoding voteobject returned unexpected error: %v", err)
			}

			if decoded.Weight != tt.expectWeight {
				t.Errorf("got weight %q, expected %q", decoded.Weight, tt.expectWeight)
			}
		})
	}
}

func TestVotedPolls(t *testing.T) {
	backend := memory.New()
	ds := dsmock.Stub(dsmock.YAMLData(`---
	poll/1/backend: memory
	`))
	v := vote.New(backend, backend, ds, &StubMessageBus{})
	backend.Start(context.Background(), 1)
	backend.Vote(context.Background(), 1, 5, []byte(`"Y"`))
	buf := new(bytes.Buffer)

	if err := v.VotedPolls(context.Background(), []int{1, 2}, 5, buf); err != nil {
		t.Fatalf("VotedPolls() returned unexected error: %v", err)
	}

	expect := `{"1":true,"2":false}` + "\n"
	if buf.String() != expect {
		t.Errorf("VotedPolls() wrote %v, expected %s", buf.String(), expect)
	}
}

func TestVoteCount(t *testing.T) {
	backend := memory.New()
	v := vote.New(backend, backend, dsmock.Stub(dsmock.YAMLData(`
	poll/1:
		meeting_id: 1
		entitled_group_ids: [1]
		pollmethod: Y
		global_yes: true
	`)), &StubMessageBus{})
	backend.Start(context.Background(), 1)
	backend.Vote(context.Background(), 1, 5, []byte(`"Y"`))
	backend.Vote(context.Background(), 1, 6, []byte(`"Y"`))
	backend.Vote(context.Background(), 1, 7, []byte(`"Y"`))
	buf := new(bytes.Buffer)

	if err := v.VoteCount(context.Background(), []int{1}, buf); err != nil {
		t.Fatalf("VoteCount() returned unexected error: %v", err)
	}

	expect := `{"poll":{"1":{"vote_count":3}}}` + "\n"
	if buf.String() != expect {
		t.Errorf("VoteCount() wrote %s, expected %s", buf.String(), expect)
	}
}
