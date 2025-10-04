package vote_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dskey"
	"github.com/OpenSlides/openslides-go/datastore/dsmock"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/OpenSlides/openslides-go/datastore/flow"
	"github.com/OpenSlides/openslides-go/datastore/pgtest"
	"github.com/OpenSlides/openslides-vote-service/vote"
)

func TestAll(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Postgres Test")
	}

	ctx := t.Context()

	pg, err := pgtest.NewPostgresTest(ctx)
	if err != nil {
		t.Fatalf("Error starting postgres: %v", err)
	}
	defer pg.Close()

	data := `---
	organization/1/enable_electronic_voting: true
	motion/5:
		meeting_id: 1
		sequential_number: 1
		title: my motion
		state_id: 1

	list_of_speakers/7:
		content_object_id: motion/5
		sequential_number: 1
		meeting_id: 1

	meeting/1:
		present_user_ids: [30]

	user:
		5:
			username: admin
			organization_management_level: superadmin
		30:
			username: tom
	meeting_user/300:
		group_ids: [40]
		user_id: 30
		meeting_id: 1

	group/40:
		name: delegate
		meeting_id: 1

	group/41:
		name: wrong group
		meeting_id: 1
	`

	withData(
		t,
		pg,
		data,
		func(service *vote.Vote, flow flow.Flow) {
			t.Run("Create", func(t *testing.T) {
				body := `{
					"title": "my pol",
					"content_object_id": "motion/5",
					"method": "motion",
					"visibility": "open",
					"meeting_id": 1,
					"entitled_group_ids": [41]
				}`

				id, err := service.Create(ctx, 5, strings.NewReader(body))
				if err != nil {
					t.Fatalf("Error creating poll: %v", err)
				}

				if id != 1 {
					t.Errorf("Expected id 1, got %d", id)
				}

				key := dskey.MustKey("poll/1/title")
				result, err := flow.Get(ctx, key)
				if err != nil {
					t.Fatalf("Error getting title from created poll: %v", err)
				}

				if string(result[key]) != `"my pol"` {
					t.Errorf("Expected title 'my poll', got %s", result[key])
				}
			})

			t.Run("Update", func(t *testing.T) {
				body := `{
					"title": "my poll",
					"entitled_group_ids": [40]
				}`

				err := service.Update(ctx, 1, 5, strings.NewReader(body))
				if err != nil {
					t.Fatalf("Error creating poll: %v", err)
				}

				poll, err := dsmodels.New(flow).Poll(1).First(ctx)
				if err != nil {
					t.Fatalf("fetch poll: %v", err)
				}

				if poll.Title != `my poll` {
					t.Errorf("Expected title 'my poll', got %s", poll.Title)
				}

				if len(poll.EntitledGroupIDs) != 1 && poll.EntitledGroupIDs[0] != 40 {
					t.Errorf("Expected entitled_group_ids [40], got %v", poll.EntitledGroupIDs)
				}
			})

			t.Run("Start", func(t *testing.T) {
				if err := service.Start(ctx, 1, 5); err != nil {
					t.Fatalf("Error starting poll: %v", err)
				}

				key := dskey.MustKey("poll/1/state")
				values, err := flow.Get(ctx, key)
				if err != nil {
					t.Fatalf("Error getting state from poll: %v", err)
				}

				if string(values[key]) != `"started"` {
					t.Errorf("Expected state to be started, got %s", values[key])
				}
			})

			t.Run("Vote", func(t *testing.T) {
				body := `{"value":"Yes"}`
				if err := service.Vote(ctx, 1, 30, strings.NewReader(body)); err != nil {
					t.Fatalf("Error voting poll: %v", err)
				}

				ds := dsmodels.New(flow)
				vote, err := ds.Vote(1).First(t.Context())
				if err != nil {
					t.Fatalf("Error: Getting vote: %v", err)
				}

				if id, _ := vote.ActingUserID.Value(); id != 30 {
					t.Errorf("Expected acting user ID to be 1, got %d", id)
				}

				if vote.Value != `"Yes"` {
					t.Errorf("Expected vote value to be '\"Yes\"', got '%s'", vote.Value)
				}
			})

			t.Run("Stop", func(t *testing.T) {
				if err := service.Finalize(ctx, 1, 5, false, false); err != nil {
					t.Fatalf("Error stopping poll: %v", err)
				}

				keyState := dskey.MustKey("poll/1/state")
				keyResult := dskey.MustKey("poll/1/result")
				values, err := flow.Get(ctx, keyState, keyResult)
				if err != nil {
					t.Fatalf("Error getting state from poll: %v", err)
				}

				if string(values[keyState]) != `"finished"` {
					t.Errorf("Expected state to be finished, got %s", values[keyState])
				}

				if string(values[keyResult]) == `` {
					t.Errorf("Expected result to be set")
				}
			})

			t.Run("Publish", func(t *testing.T) {
				if err := service.Finalize(ctx, 1, 5, true, false); err != nil {
					t.Fatalf("Error publishing poll: %v", err)
				}

				key := dskey.MustKey("poll/1/published")
				values, err := flow.Get(ctx, key)
				if err != nil {
					t.Fatalf("Error getting state from poll: %v", err)
				}

				if string(values[key]) != `true` {
					t.Errorf("Expected published to be true, got %s", values[key])
				}
			})

			t.Run("Anonymize", func(t *testing.T) {
				if err := service.Finalize(ctx, 1, 5, true, true); err != nil {
					t.Fatalf("Error anonymizing poll: %v", err)
				}

				ds := dsmodels.New(flow)
				vote, err := ds.Vote(1).First(t.Context())
				if err != nil {
					t.Fatalf("Error: Getting vote: %v", err)
				}

				if id, set := vote.ActingUserID.Value(); set {
					t.Errorf("Expected acting user ID not to be set, but is is %d", id)
				}
			})

			t.Run("Reset", func(t *testing.T) {
				if err := service.Reset(ctx, 1, 5); err != nil {
					t.Fatalf("Error resetting poll: %v", err)
				}

				key := dskey.MustKey("poll/1/state")
				values, err := flow.Get(ctx, key)
				if err != nil {
					t.Fatalf("Error getting state from poll: %v", err)
				}

				if string(values[key]) != `"created"` {
					t.Errorf("Expected state to be created, got %s", values[key])
				}
			})

			t.Run("Delete", func(t *testing.T) {
				if err := service.Delete(ctx, 1, 5); err != nil {
					t.Fatalf("Error deleting poll: %v", err)
				}

				key := dskey.MustKey("poll/1/title")
				_, err := flow.Get(ctx, key)
				if err != nil {
					t.Fatalf("Error getting title from created poll: %v", err)
				}
			})

		},
	)
}

func TestManually(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Postgres Test")
	}

	ctx := t.Context()

	pg, err := pgtest.NewPostgresTest(ctx)
	if err != nil {
		t.Fatalf("Error starting postgres: %v", err)
	}
	defer pg.Close()

	data := `---
	user/5:
		username: admin
		organization_management_level: superadmin

	motion/5:
		meeting_id: 1
		sequential_number: 1
		title: my motion
		state_id: 1

	list_of_speakers/7:
		content_object_id: motion/5
		sequential_number: 1
		meeting_id: 1

	meeting/1/welcome_title: hello world
	`

	withData(t, pg, data, func(service *vote.Vote, flow flow.Flow) {
		t.Run("Create", func(t *testing.T) {
			body := `{
				"title": "my poll",
				"content_object_id": "motion/5",
				"method": "motion",
				"visibility": "manually",
				"meeting_id": 1,
				"result": {"no":"23","yes":"42"}
			}`

			id, err := service.Create(ctx, 5, strings.NewReader(body))
			if err != nil {
				t.Fatalf("Error creating poll: %v", err)
			}

			if id != 1 {
				t.Errorf("Expected id 1, got %d", id)
			}

			poll, err := dsmodels.New(flow).Poll(1).First(ctx)
			if err != nil {
				t.Fatalf("Fetch poll: %v", err)
			}

			if poll.State != "finished" {
				t.Errorf("Poll is in state %s, expected state finished", poll.State)
			}

			if poll.Result != `{"no":"23","yes":"42"}` {
				t.Errorf("Result does not match")
			}
		})

		t.Run("Reset", func(t *testing.T) {
			err := service.Reset(ctx, 1, 5)
			if err != nil {
				t.Fatalf("Error creating poll: %v", err)
			}

			poll, err := dsmodels.New(flow).Poll(1).First(ctx)
			if err != nil {
				t.Fatalf("Fetch poll: %v", err)
			}

			if poll.State != "finished" {
				t.Errorf("State == %s. A manually poll has to be in state finished after a reset", poll.State)
			}
		})
	})
}

func TestVote(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Postgres Test")
	}

	ctx := t.Context()

	pg, err := pgtest.NewPostgresTest(ctx)
	if err != nil {
		t.Fatalf("Error starting postgres: %v", err)
	}
	defer pg.Close()

	data := `---
	motion/5:
		meeting_id: 1
		sequential_number: 1
		title: my motion
		state_id: 1

	list_of_speakers/7:
		content_object_id: motion/5
		sequential_number: 1
		meeting_id: 1

	meeting/1:
		present_user_ids: [30]

	user/30:
		username: tom
	meeting_user/300:
		group_ids: [40]
		user_id: 30
		meeting_id: 1

	group/40:
		name: delegate
		meeting_id: 1

	poll/5:
		title: my poll
		method: motion
		visibility: open
		sequential_number: 1
		content_object_id: motion/5
		meeting_id: 1
		state: started
		entitled_group_ids: [40]
	`

	withData(
		t,
		pg,
		data,
		func(service *vote.Vote, flow flow.Flow) {
			t.Run("Simple Vote", func(t *testing.T) {
				defer pg.Cleanup(t)

				body := `{"value":"Yes"}`
				if err := service.Vote(ctx, 5, 30, strings.NewReader(body)); err != nil {
					t.Fatalf("Error processing poll: %v", err)
				}

				ds := dsmodels.New(flow)
				vote, err := ds.Vote(1).First(t.Context())
				if err != nil {
					t.Fatalf("Error: Getting vote: %v", err)
				}

				if id, _ := vote.ActingUserID.Value(); id != 30 {
					t.Errorf("Expected acting user ID to be 1, got %d", id)
				}

				if vote.Value != `"Yes"` {
					t.Errorf("Expected vote value to be 'Yes', got '%s'", vote.Value)
				}
			})
		},
	)
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
				method: motion
				visibility: open
				content_object_id: some_field/1
				sequential_number: 1
				title: myPoll

			meeting/1/id: 1

			user/1:
				is_present_in_meeting_ids: [1]
				meeting_user_ids: [10]
			meeting_user/10:
				group_ids: [1]
				meeting_id: 1
			`,
			"1.000000",
		},
		{
			"Weight enabled, user has no weight",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				method: motion
				visibility: open
				content_object_id: some_field/1
				sequential_number: 1
				title: myPoll

			meeting/1/users_enable_vote_weight: true

			user/1:
				is_present_in_meeting_ids: [1]
				meeting_user_ids: [10]
			meeting_user/10:
				group_ids: [1]
				meeting_id: 1
			`,
			"1.000000",
		},
		{
			"Weight enabled, user has default weight",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				method: motion
				visibility: open
				content_object_id: some_field/1
				sequential_number: 1
				title: myPoll

			meeting/1/users_enable_vote_weight: true

			user/1:
				is_present_in_meeting_ids: [1]
				meeting_user_ids: [10]
				default_vote_weight: "2.000000"
			meeting_user/10:
				group_ids: [1]
				meeting_id: 1
			`,
			"2.000000",
		},
		{
			"Weight enabled, user has default weight and meeting weight",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				method: motion
				visibility: open
				content_object_id: some_field/1
				sequential_number: 1
				title: myPoll

			meeting/1/users_enable_vote_weight: true

			user/1:
				is_present_in_meeting_ids: [1]
				meeting_user_ids: [10]
				default_vote_weight: "2.000000"
			meeting_user/10:
				group_ids: [1]
				meeting_id: 1
				vote_weight: "3.000000"
			`,
			"3.000000",
		},
		{
			"Weight enabled, user has default weight and meeting weight in other meeting",
			`
			poll/1:
				meeting_id: 1
				entitled_group_ids: [1]
				method: motion
				visibility: open
				content_object_id: some_field/1
				sequential_number: 1
				title: myPoll

			meeting/1/users_enable_vote_weight: true

			user/1:
				is_present_in_meeting_ids: [1]
				meeting_user_ids: [10,11]
				default_vote_weight: "2.000000"
			meeting_user/10:
				group_ids: [1]
				meeting_id: 1
			meeting_user/11:
				group_ids: [1]
				meeting_id: 2
				vote_weight: "3.000000"
			`,
			"2.000000",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ds := dsfetch.New(dsmock.Stub(dsmock.YAMLData(tt.data)))
			weight, err := vote.CalcVoteWeight(t.Context(), ds, 1, 1)
			if err != nil {
				t.Fatalf("CalcVote: %v", err)
			}

			if weight != tt.expectWeight {
				t.Errorf("got weight %q, expected %q", weight, tt.expectWeight)
			}
		})
	}
}

func TestVoteStart(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Postgres Test")
	}

	ctx := t.Context()

	pg, err := pgtest.NewPostgresTest(ctx)
	if err != nil {
		t.Fatalf("Error starting postgres: %v", err)
	}
	defer pg.Close()

	data := `---
	motion/5:
		meeting_id: 1
		sequential_number: 1
		title: my motion
		state_id: 1

	list_of_speakers/7:
		content_object_id: motion/5
		sequential_number: 1
		meeting_id: 1

	meeting/1:
		present_user_ids: [30]

	user:
		30:
			username: tom
		5:
			username: admin
			organization_management_level: superadmin

	meeting_user/300:
		group_ids: [40]
		user_id: 30
		meeting_id: 1

	group/40:
		name: delegate
		meeting_id: 1

	poll/5:
		title: normal poll
		method: motion
		visibility: open
		sequential_number: 1
		content_object_id: motion/5
		meeting_id: 1
		state: created
		entitled_group_ids: [40]

	poll/6:
		title: manually poll
		method: motion
		visibility: manually
		sequential_number: 2
		content_object_id: motion/5
		meeting_id: 1
		state: created
	`

	withData(
		t,
		pg,
		data,
		func(service *vote.Vote, flow flow.Flow) {
			t.Run("Unknown poll", func(t *testing.T) {
				err := service.Start(ctx, 404, 5)
				if !errors.Is(err, vote.ErrNotExists) {
					t.Errorf("Start returned unexpected error: %v", err)
				}
			})

			t.Run("Not started poll", func(t *testing.T) {
				if err := service.Start(ctx, 5, 5); err != nil {
					t.Errorf("Start returned unexpected error: %v", err)
				}
			})

			t.Run("Start poll a second time", func(t *testing.T) {
				if err := service.Start(ctx, 5, 5); err != nil {
					t.Errorf("Start returned unexpected error: %v", err)
				}
			})

			t.Run("Start a finished poll", func(t *testing.T) {
				if err := service.Finalize(ctx, 5, 5, false, false); err != nil {
					t.Errorf("Stop poll")
				}

				err := service.Start(ctx, 5, 5)
				if !errors.Is(err, vote.ErrInvalid) {
					t.Errorf("Start returned unexpected error: %v", err)
				}
			})

			t.Run("Start an anolog poll", func(t *testing.T) {
				err := service.Start(ctx, 6, 5)
				if !errors.Is(err, vote.ErrInvalid) {
					t.Errorf("Start returned unexpected error: %v", err)
				}
			})
		},
	)
}

func TestVoteFinalize(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Postgres Test")
	}

	ctx := t.Context()

	pg, err := pgtest.NewPostgresTest(ctx)
	if err != nil {
		t.Fatalf("Error starting postgres: %v", err)
	}
	defer pg.Close()

	data := `---
	motion/5:
		meeting_id: 1
		sequential_number: 1
		title: my motion
		state_id: 1

	list_of_speakers/7:
		content_object_id: motion/5
		sequential_number: 1
		meeting_id: 1

	meeting/1:
		present_user_ids: [30]

	user:
		30:
			username: tom
		5:
			username: admin
			organization_management_level: superadmin

	meeting_user/300:
		group_ids: [40]
		user_id: 30
		meeting_id: 1

	group/40:
		name: delegate
		meeting_id: 1

	poll/5:
		title: poll with votes
		method: motion
		visibility: open
		sequential_number: 1
		content_object_id: motion/5
		meeting_id: 1
		state: started
		entitled_group_ids: [40]

	vote/1:
		poll_id: 5
		value: '"yes"'
		represented_user_id: 30
	vote/2:
		poll_id: 5
		value: '"no"'
		represented_user_id: 5
	`

	withData(
		t,
		pg,
		data,
		func(service *vote.Vote, flow flow.Flow) {
			t.Run("Unknown poll", func(t *testing.T) {
				err := service.Finalize(ctx, 404, 5, false, false)
				if !errors.Is(err, vote.ErrNotExists) {
					t.Errorf("Stopping an unknown poll has to return an ErrNotExists, got: %v", err)
				}
			})

			t.Run("Poll with votes", func(t *testing.T) {
				if err := service.Finalize(ctx, 5, 5, false, false); err != nil {
					t.Fatalf("Stop returned unexpected error: %v", err)
				}

				poll, err := dsmodels.New(flow).Poll(5).First(ctx)
				if err != nil {
					t.Fatalf("load poll after finalize: %v", err)
				}

				if poll.Result != `{"no":"1","yes":"1"}` {
					t.Errorf("Got result %s, expected %s", poll.Result, `{"no":"1","yes":"1"}`)
				}

				if poll.State != "finished" {
					t.Errorf("Poll state is %s, expected finished", poll.State)
				}

				if slices.Compare(poll.VotedIDs, []int{30, 5}) == 0 {
					t.Errorf("VotedIDs are %v, expected %v", poll.VotedIDs, []int{30, 5})
				}
			})

			t.Run("finish poll a second time", func(t *testing.T) {
				if err := service.Finalize(ctx, 5, 5, false, false); err != nil {
					t.Fatalf("Stop returned unexpected error: %v", err)
				}

				poll, err := dsmodels.New(flow).Poll(5).First(ctx)
				if err != nil {
					t.Fatalf("load poll after finalize: %v", err)
				}

				if poll.State != "finished" {
					t.Errorf("Poll state is %s, expected finished", poll.State)
				}
			})
		},
	)
}

func TestVoteVote(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Postgres Test")
	}

	ctx := t.Context()

	pg, err := pgtest.NewPostgresTest(ctx)
	if err != nil {
		t.Fatalf("Error starting postgres: %v", err)
	}
	defer pg.Close()

	data := `---
	motion/5:
		meeting_id: 1
		sequential_number: 1
		title: my motion
		state_id: 1

	list_of_speakers/7:
		content_object_id: motion/5
		sequential_number: 1
		meeting_id: 1

	meeting/1:
		present_user_ids: [30]

	user:
		30:
			username: tom
		5:
			username: admin
			organization_management_level: superadmin

	meeting_user/300:
		group_ids: [40]
		user_id: 30
		meeting_id: 1

	group/40:
		name: delegate
		meeting_id: 1

	poll/5:
		title: poll with votes
		method: motion
		visibility: open
		sequential_number: 1
		content_object_id: motion/5
		meeting_id: 1
		state: started
		entitled_group_ids: [40]
	`

	withData(
		t,
		pg,
		data,
		func(service *vote.Vote, flow flow.Flow) {
			t.Run("Poll does not exist in DS", func(t *testing.T) {
				err := service.Vote(ctx, 404, 1, strings.NewReader(`{"value":"Y"}`))
				if !errors.Is(err, vote.ErrNotExists) {
					t.Errorf("Expected ErrNotExists, got: %v", err)
				}
			})

			t.Run("Invalid json", func(t *testing.T) {
				err := service.Vote(ctx, 5, 30, strings.NewReader(`{123`))

				var errTyped vote.TypeError
				if !errors.As(err, &errTyped) {
					t.Fatalf("Vote() did not return an TypeError, got: %v", err)
				}

				if errTyped != vote.ErrInvalid {
					t.Errorf("Got error type `%s`, expected `%s`", errTyped.Type(), vote.ErrInvalid.Type())
				}
			})

			t.Run("Invalid format", func(t *testing.T) {
				err := service.Vote(ctx, 5, 30, strings.NewReader(`{}`))

				var errTyped vote.TypeError
				if !errors.As(err, &errTyped) {
					t.Fatalf("Vote() did not return an TypeError, got: %v", err)
				}

				if errTyped != vote.ErrInvalid {
					t.Errorf("Got error type `%s`, expected `%s`", errTyped.Type(), vote.ErrInvalid.Type())
				}
			})

			t.Run("Valid data", func(t *testing.T) {
				err := service.Vote(ctx, 5, 30, strings.NewReader(`{"value":"Yes"}`))
				if err != nil {
					t.Fatalf("Vote returned unexpected error: %v", err)
				}
			})

			t.Run("User has voted", func(t *testing.T) {
				err := service.Vote(ctx, 5, 30, strings.NewReader(`{"value":"Yes"}`))
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
				if err := service.Finalize(ctx, 5, 5, false, false); err != nil {
					t.Fatalf("Finalize poll: %v", err)
				}

				err := service.Vote(ctx, 5, 30, strings.NewReader(`{"value":"Yes"}`))
				if err == nil {
					t.Fatalf("Vote returned no error")
				}

				var errTyped vote.TypeError
				if !errors.As(err, &errTyped) {
					t.Fatalf("Vote() did not return an TypeError, got: %v", err)
				}

				if errTyped != vote.ErrNotStarted {
					t.Errorf("Got error type `%s`, expected `%s`", errTyped.Type(), vote.ErrNotStarted.Type())
				}
			})
		},
	)
}

func TestVoteDelegationAndGroup(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Postgres Test")
	}

	ctx := t.Context()

	pg, err := pgtest.NewPostgresTest(ctx)
	if err != nil {
		t.Fatalf("Error starting postgres: %v", err)
	}
	defer pg.Close()

	baseData := `
	meeting/1/users_enable_vote_delegations: true

	motion/5:
		meeting_id: 1
		sequential_number: 1
		title: my motion
		state_id: 1

	list_of_speakers/7:
		content_object_id: motion/5
		sequential_number: 1
		meeting_id: 1

	group/40:
		name: delegates
		meeting_id: 1

	user:
		5:
			username: admin
			organization_management_level: superadmin
		30:
			username: tom

		40:
			username: georg

	meeting_user:
		31:
			user_id: 30
			meeting_id: 1

		41:
			user_id: 40
			meeting_id: 1

	poll/5:
		title: normal poll
		method: motion
		visibility: open
		sequential_number: 1
		content_object_id: motion/5
		meeting_id: 1
		state: started
		entitled_group_ids: [40]

	`

	for _, tt := range []struct {
		name string
		data string
		vote string

		expectVotedUserID int
	}{
		{
			"Not delegated",
			`
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user/31:
				group_ids: [40]
			`,
			`{"value":"Yes"}`,

			30,
		},

		{
			"Not delegated not present",
			`
			meeting_user/31:
				group_ids: [40]
			`,
			`{"value":"Yes"}`,

			0,
		},

		{
			"Not delegated not in group",
			`
			user/30:
				is_present_in_meeting_ids: [1]
			`,
			`{"value":"Yes"}`,

			0,
		},

		{
			"Vote for self",
			`
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user/31:
				group_ids: [40]
			`,
			`{"user_id": 30, "value":"Yes"}`,

			30,
		},

		{
			"Vote for self not activated",
			`
			meeting/1/users_enable_vote_delegations: false
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user/31:
				group_ids: [40]
			`,
			`{"user_id": 30, "value":"Yes"}`,

			30,
		},

		{
			"Vote for anonymous",
			`
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user/31:
				group_ids: [40]
			`,
			`{"user_id": 0, "value":"Yes"}`,

			0,
		},

		{
			"Vote for other without delegation",
			`
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user/31:
				group_ids: [40]
			`,
			`{"user_id": 40, "value":"Yes"}`,

			0,
		},

		{
			"Vote for other with delegation",
			`
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user:
				41:
					group_ids: [40]
					vote_delegated_to_id: 31
			`,
			`{"user_id": 40, "value":"Yes"}`,

			40,
		},

		{
			"Vote for other with delegation not activated",
			`
			meeting/1/users_enable_vote_delegations: false
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user:
				41:
					group_ids: [40]
					vote_delegated_to_id: 31
			`,
			`{"user_id": 40, "value":"Yes"}`,

			0,
		},

		{
			"Vote for other with delegation not in group",
			`
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user:
				41:
					vote_delegated_to_id: 31
			`,
			`{"user_id": 40, "value":"Yes"}`,

			0,
		},

		{
			"Vote for self when delegation is activated users_forbid_delegator_to_vote==false",
			`
			meeting/1/users_forbid_delegator_to_vote: false
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user/31:
				group_ids: [40]
				vote_delegated_to_id: 41
			`,
			`{"user_id": 30, "value":"Yes"}`,

			30,
		},

		{
			"Vote for self when delegation is activated users_forbid_delegator_to_vote==true",
			`
			meeting/1/users_forbid_delegator_to_vote: true
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user/31:
				group_ids: [40]
				vote_delegated_to_id: 41
			`,
			`{"user_id": 30, "value":"Yes"}`,

			0,
		},

		{
			"Vote for self when delegation is deactivated users_forbid_delegator_to_vote==true",
			`
			meeting/1:
				users_forbid_delegator_to_vote: true
				users_enable_vote_delegations: false

			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user/31:
				group_ids: [40]
				vote_delegated_to_id: 41
			`,
			`{"user_id": 30, "value":"Yes"}`,

			30,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pg.Cleanup(t)

			if err := pg.AddData(ctx, baseData); err != nil {
				t.Fatalf("Insert base data: %v", err)
			}

			withData(
				t,
				pg,
				tt.data,
				func(service *vote.Vote, flow flow.Flow) {
					err := service.Vote(ctx, 5, 30, strings.NewReader(tt.vote))

					if tt.expectVotedUserID != 0 {
						if err != nil {
							t.Fatalf("Vote returned unexpected error: %v", err)
						}

						ds := dsmodels.New(flow)
						q := ds.Poll(5)
						q = q.Preload(q.VoteList())
						poll, err := q.First(ctx)
						if err != nil {
							t.Fatalf("Error: Getting votes from poll: %v", err)
						}
						found := slices.ContainsFunc(poll.VoteList, func(vote dsmodels.Vote) bool {
							userID, _ := vote.RepresentedUserID.Value()
							return userID == tt.expectVotedUserID
						})

						if !found {
							t.Errorf("user %d has not voted", tt.expectVotedUserID)
						}

						return
					}

					if !errors.Is(err, vote.ErrNotAllowed) {
						t.Fatalf("Expected NotAllowedError, got: %v", err)
					}
				},
			)
		})
	}
}

func withData(t *testing.T, pg *pgtest.PostgresTest, data string, fn func(service *vote.Vote, flow flow.Flow)) {
	t.Helper()

	ctx := t.Context()

	if err := pg.AddData(ctx, data); err != nil {
		t.Fatalf("Error: inserting data: %v", err)
	}

	flow, err := pg.Flow()
	if err != nil {
		t.Fatalf("Error getting flow: %v", err)
	}
	defer flow.Close()

	conn, err := pg.Conn(ctx)
	if err != nil {
		t.Fatalf("Error getting connection: %v", err)
	}
	defer conn.Close(ctx)

	service, _, err := vote.New(ctx, flow, conn)
	if err != nil {
		t.Fatalf("Error creating vote: %v", err)
	}

	fn(service, flow)
}
