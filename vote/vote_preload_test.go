package vote_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/OpenSlides/openslides-go/datastore/cache"
	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dsmock"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/OpenSlides/openslides-go/datastore/pgtest"
	"github.com/OpenSlides/openslides-vote-service/vote"
)

func TestVoteNoRequests(t *testing.T) {
	// This tests makes sure, that a request to vote does not do any reading
	// from the database. All values have to be in the cache from pollpreload.

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
		method: approval
		visibility: open
		sequential_number: 1
		content_object_id: motion/5
		meeting_id: 1
		state: created
		entitled_group_ids: [40]
	`

	for _, tt := range []struct {
		name              string
		data              string
		vote              string
		expectVotedUserID int
	}{
		{
			"normal vote",
			`---
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user/31:
				group_ids: [40]

			`,
			`{"value":"Yes"}`,
			30,
		},
		{
			"delegation vote",
			`---
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user:
				41:
					group_ids: [40]
					vote_delegated_to_id: 31

			`,
			`{"user_id":40,"value":"Yes"}`,
			40,
		},
		{
			"vote weight enabled",
			`---
			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user/31:
				group_ids: [40]

			meeting/1:
				users_enable_vote_weight: true
			`,
			`{"value":"Yes"}`,
			30,
		},
		{
			"vote weight enabled and delegated",
			`---
			meeting/1:
				users_enable_vote_weight: true

			user/30:
				is_present_in_meeting_ids: [1]

			meeting_user:
				41:
					group_ids: [40]
					vote_delegated_to_id: 31
			`,
			`{"user_id":40,"value":"Yes"}`,
			40,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pg.Cleanup(t)

			if err := pg.AddData(ctx, baseData); err != nil {
				t.Fatalf("Error: Insert base data: %v", err)
			}

			if err := pg.AddData(ctx, tt.data); err != nil {
				t.Fatalf("Error: Inserting data: %v", err)
			}

			flow, err := pg.Flow()
			if err != nil {
				t.Fatalf("Error getting flow: %v", err)
			}
			defer flow.Close()

			counter := dsmock.NewCounterFlow(flow)
			cache := cache.New(counter)

			conn, err := pg.Conn(ctx)
			if err != nil {
				t.Fatalf("Error getting connection: %v", err)
			}
			defer conn.Close(ctx)

			service, _, err := vote.New(ctx, cache, conn)
			if err != nil {
				t.Fatalf("Error creating vote: %v", err)
			}

			if err := service.Start(ctx, 5, 5); err != nil {
				t.Fatalf("Start poll: %v", err)
			}
			counter.Reset()

			if err := service.Vote(ctx, 5, 30, strings.NewReader(tt.vote)); err != nil {
				t.Fatalf("Vote returned unexpected error: %v", err)
			}

			if counter.Count() != 0 {
				t.Errorf("Vote send %d requests to the datastore:\n%s", counter.Count(), counter.PrintRequests())
			}

			ds := dsmodels.New(counter) // Use the counter here to skip the cache
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
		})
	}
}

func TestPreload(t *testing.T) {
	// Tests, that the preload function needs a specific number of requests to
	// postgres.
	ctx := t.Context()

	for _, tt := range []struct {
		name        string
		data        string
		expectCount int
	}{
		{
			"one user",
			`---
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
				meeting_user_ids: [30]

			user:
				5:
					username: admin
					organization_management_level: superadmin
				30:
					username: tom

			meeting_user:
				30:
					user_id: 30
					meeting_id: 1
					group_ids: 40

			poll/5:
				title: normal poll
				method: approval
				visibility: open
				sequential_number: 1
				content_object_id: motion/5
				meeting_id: 1
				state: created
				entitled_group_ids: [40]
			`,
			3,
		},

		{
			"Many groups",
			`---
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
				meeting_user_ids: [30]
			group/41:
				name: delegates2
				meeting_id: 1
				meeting_user_ids: [30]

			user:
				5:
					username: admin
					organization_management_level: superadmin
				30:
					username: tom

			meeting_user:
				30:
					user_id: 30
					meeting_id: 1
					group_ids: 40

			poll/5:
				title: normal poll
				method: approval
				visibility: open
				sequential_number: 1
				content_object_id: motion/5
				meeting_id: 1
				state: created
				entitled_group_ids: [40,41]
			`,
			3,
		},

		{
			"Many users",
			`---
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
				meeting_user_ids: [30,31]

			user:
				5:
					username: admin
					organization_management_level: superadmin
				30:
					username: tom
				31:
					username: gregor

			meeting_user:
				30:
					user_id: 30
					meeting_id: 1
					group_ids: 40
				31:
					user_id: 30
					meeting_id: 1
					group_ids: 40

			poll/5:
				title: normal poll
				method: approval
				visibility: open
				sequential_number: 1
				content_object_id: motion/5
				meeting_id: 1
				state: created
				entitled_group_ids: [40]
			`,
			3,
		},

		{
			"Many users in different groups",
			`---
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
				meeting_user_ids: [30]

			group/41:
				name: delegates
				meeting_id: 1
				meeting_user_ids: [31]

			user:
				5:
					username: admin
					organization_management_level: superadmin
				30:
					username: tom
				31:
					username: gregor

			meeting_user:
				30:
					user_id: 30
					meeting_id: 1
					group_ids: 40
				31:
					user_id: 30
					meeting_id: 1
					group_ids: 41

			poll/5:
				title: normal poll
				method: approval
				visibility: open
				sequential_number: 1
				content_object_id: motion/5
				meeting_id: 1
				state: created
				entitled_group_ids: [40,41]
			`,
			3,
		},

		{
			"Many users in different groups with delegation",
			`---
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
				meeting_user_ids: [500]

			group/41:
				name: delegates
				meeting_id: 1
				meeting_user_ids: [510]

			user:
				50:
					username: user50
					organization_id: 1
					is_present_in_meeting_ids: [5]

				51:
					username: user51
					organization_id: 1
					is_present_in_meeting_ids: [5]

				52:
					username: user52
					organization_id: 1
					is_present_in_meeting_ids: [5]

				53:
					username: user53
					organization_id: 1
					is_present_in_meeting_ids: [5]

			meeting_user:
				500:
					user_id: 50
					vote_delegated_to_id: 520
					meeting_id: 5
				510:
					user_id: 51
					vote_delegated_to_id: 530
					meeting_id: 5
				520:
					user_id: 52
					meeting_id: 5
				530:
					user_id: 53
					meeting_id: 5

			poll/5:
				title: normal poll
				method: approval
				visibility: open
				sequential_number: 1
				content_object_id: motion/5
				meeting_id: 1
				state: created
				entitled_group_ids: [40,41]
			`,
			4,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dsCount := dsmock.NewCounter(dsmock.Stub(dsmock.YAMLData(tt.data)))
			ds := dsmock.NewCache(dsCount)
			fetcher := dsmodels.New(ds)

			poll, err := fetcher.Poll(5).First(ctx)
			if err != nil {
				t.Fatalf("loadPoll returned: %v", err)
			}

			dsCount.Reset()

			if err := vote.Preload(ctx, dsfetch.New(ds), poll); err != nil {
				t.Errorf("preload returned: %v", err)
			}

			if got := dsCount.Count(); got != tt.expectCount {
				t.Errorf("preload send %d requests, expected %d:\n%s", got, tt.expectCount, dsCount.PrintRequests())
			}
		})
	}
}
