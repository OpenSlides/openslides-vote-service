package vote

// import (
// 	"bytes"
// 	"context"
// 	"fmt"
// 	"testing"

// 	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
// 	"github.com/OpenSlides/openslides-go/datastore/dsmock"
// 	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
// )
//
//
// func TestVoteNoRequests(t *testing.T) {
// 	// This tests makes sure, that a request to vote does not do any reading
// 	// from the database. All values have to be in the cache from pollpreload.

// 	for _, tt := range []struct {
// 		name              string
// 		data              string
// 		vote              string
// 		expectVotedUserID int
// 	}{
// 		{
// 			"normal vote",
// 			`---
// 			poll/1:
// 				meeting_id: 50
// 				entitled_group_ids: [5]
// 				pollmethod: Y
// 				global_yes: true
// 				state: started
// 				backend: fast
// 				type: pseudoanonymous
// 				content_object_id: some_field/1
// 				sequential_number: 1
// 				onehundred_percent_base: base
// 				title: myPoll

// 			meeting/50/users_enable_vote_delegations: true

// 			user/1:
// 				is_present_in_meeting_ids: [50]
// 				meeting_user_ids: [10]
// 			meeting_user/10:
// 				meeting_id: 50
// 				group_ids: [5]
// 				user_id: 1

// 			group/5/meeting_user_ids: [10]
// 			`,
// 			`{"value":"Y"}`,
// 			1,
// 		},
// 		{
// 			"delegation vote",
// 			`---
// 			poll/1:
// 				meeting_id: 50
// 				entitled_group_ids: [5]
// 				pollmethod: Y
// 				global_yes: true
// 				state: started
// 				backend: fast
// 				type: pseudoanonymous
// 				content_object_id: some_field/1
// 				sequential_number: 1
// 				onehundred_percent_base: base
// 				title: myPoll

// 			meeting/50/users_enable_vote_delegations: true

// 			user:
// 				1:
// 					is_present_in_meeting_ids: [50]
// 					meeting_user_ids: [10]
// 				2:
// 					meeting_user_ids: [20]

// 			meeting_user:
// 				10:
// 					user_id: 1
// 					vote_delegations_from_ids: [20]
// 					meeting_id: 50
// 				20:
// 					meeting_id: 50
// 					vote_delegated_to_id: 10
// 					group_ids: [5]
// 					user_id: 2

// 			group/5/meeting_user_ids: [20]
// 			`,
// 			`{"user_id":2,"value":"Y"}`,
// 			2,
// 		},
// 		{
// 			"vote weight enabled",
// 			`---
// 			poll/1:
// 				meeting_id: 50
// 				entitled_group_ids: [5]
// 				pollmethod: Y
// 				global_yes: true
// 				state: started
// 				backend: fast
// 				type: pseudoanonymous
// 				content_object_id: some_field/1
// 				sequential_number: 1
// 				onehundred_percent_base: base
// 				title: myPoll

// 			meeting/50:
// 				users_enable_vote_weight: true
// 				users_enable_vote_delegations: true

// 			user/1:
// 				is_present_in_meeting_ids: [50]
// 				meeting_user_ids: [10]

// 			meeting_user:
// 				10:
// 					group_ids: [5]
// 					user_id: 1
// 					meeting_id: 50

// 			group/5/meeting_user_ids: [10]
// 			`,
// 			`{"value":"Y"}`,
// 			1,
// 		},
// 		{
// 			"vote weight enabled and delegated",
// 			`---
// 			poll/1:
// 				meeting_id: 50
// 				entitled_group_ids: [5]
// 				pollmethod: Y
// 				global_yes: true
// 				state: started
// 				backend: fast
// 				type: pseudoanonymous
// 				content_object_id: some_field/1
// 				sequential_number: 1
// 				onehundred_percent_base: base
// 				title: myPoll

// 			meeting/50:
// 				users_enable_vote_weight: true
// 				users_enable_vote_delegations: true

// 			user:
// 				1:
// 					is_present_in_meeting_ids: [50]
// 					meeting_user_ids: [10]
// 				2:
// 					meeting_user_ids: [20]

// 			meeting_user:
// 				10:
// 					meeting_id: 50
// 					user_id: 1

// 				20:
// 					group_ids: [5]
// 					meeting_id: 50
// 					user_id: 2
// 					vote_delegated_to_id: 10

// 			group/5/meeting_user_ids: [20]
// 			`,
// 			`{"user_id":2,"value":"Y"}`,
// 			2,
// 		},
// 	} {
// 		t.Run(tt.name, func(t *testing.T) {
// 			ctx := context.Background()
// 			ds := dsmock.NewFlow(
// 				dsmock.YAMLData(tt.data),
// 				dsmock.NewCounter,
// 			)
// 			counter := ds.Middlewares()[0].(*dsmock.Counter)
// 			cachedDS := cache.New(ds)
// 			backend := memory.New()
// 			v, _, _ := vote.New(ctx, backend, backend, cachedDS, true)

// 			if err := v.Start(ctx, 1); err != nil {
// 				t.Fatalf("Can not start poll: %v", err)
// 			}

// 			counter.Reset()

// 			if err := v.Vote(ctx, 1, 1, strings.NewReader(tt.vote)); err != nil {
// 				t.Errorf("Vote returned unexpected error: %v", err)
// 			}

// 			if counter.Count() != 0 {
// 				t.Errorf("Vote send %d requests to the datastore: %v", counter.Count(), counter.Requests())
// 			}

// 			backend.AssertUserHasVoted(t, 1, tt.expectVotedUserID)
// 		})
// 	}
// }

// func TestPreload(t *testing.T) {
// 	// Tests, that the preload function needs a specific number of requests to
// 	// postgres.
// 	ctx := context.Background()

// 	for _, tt := range []struct {
// 		name        string
// 		data        string
// 		expectCount int
// 	}{
// 		{
// 			"one user",
// 			`---
// 			meeting/5/id: 5
// 			poll/1:
// 				meeting_id: 5
// 				entitled_group_ids: [30]
// 				pollmethod: Y
// 				global_yes: true
// 				backend: fast
// 				type: pseudoanonymous
// 				content_object_id: some_field/1
// 				sequential_number: 1
// 				onehundred_percent_base: base
// 				title: myPoll

// 			group/30/meeting_user_ids: [500]

// 			user/50:
// 				is_present_in_meeting_ids: [5]

// 			meeting_user/500:
// 				group_ids: [31]
// 				user_id: 50
// 				meeting_id: 5
// 			`,
// 			3,
// 		},

// 		{
// 			"Many groups",
// 			`---
// 			meeting/5/id: 5
// 			poll/1:
// 				meeting_id: 5
// 				entitled_group_ids: [30,31]
// 				pollmethod: Y
// 				global_yes: true
// 				backend: fast
// 				type: pseudoanonymous
// 				content_object_id: some_field/1
// 				sequential_number: 1
// 				onehundred_percent_base: base
// 				title: myPoll

// 			group/30/meeting_user_ids: [500]
// 			group/31/meeting_user_ids: [500]

// 			user:
// 				50:
// 					is_present_in_meeting_ids: [5]

// 			meeting_user/500:
// 				user_id: 50
// 				group_ids: [30]
// 				meeting_id: 5
// 			`,
// 			3,
// 		},

// 		{
// 			"Many users",
// 			`---
// 			meeting/5/id: 5
// 			poll/1:
// 				meeting_id: 5
// 				entitled_group_ids: [30]
// 				pollmethod: Y
// 				global_yes: true
// 				backend: fast
// 				type: pseudoanonymous
// 				content_object_id: some_field/1
// 				sequential_number: 1
// 				onehundred_percent_base: base
// 				title: myPoll

// 			group/30/meeting_user_ids: [500,510]

// 			user:
// 				50:
// 					is_present_in_meeting_ids: [5]

// 				51:
// 					is_present_in_meeting_ids: [5]

// 			meeting_user:
// 				500:
// 					user_id: 50
// 					meeting_id: 5
// 				510:
// 					user_id: 51
// 					meeting_id: 5
// 			`,
// 			3,
// 		},

// 		{
// 			"Many users in different groups",
// 			`---
// 			meeting/5/id: 5
// 			poll/1:
// 				meeting_id: 5
// 				entitled_group_ids: [30, 31]
// 				pollmethod: Y
// 				global_yes: true
// 				backend: fast
// 				type: pseudoanonymous
// 				content_object_id: some_field/1
// 				sequential_number: 1
// 				onehundred_percent_base: base
// 				title: myPoll

// 			group/30/meeting_user_ids: [500]
// 			group/31/meeting_user_ids: [510]

// 			user:
// 				50:
// 					is_present_in_meeting_ids: [5]

// 				51:
// 					is_present_in_meeting_ids: [5]

// 			meeting_user:
// 				500:
// 					user_id: 50
// 					meeting_id: 5
// 				510:
// 					user_id: 51
// 					meeting_id: 5
// 			`,
// 			3,
// 		},

// 		{
// 			"Many users in different groups with delegation",
// 			`---
// 			meeting/5/id: 5
// 			poll/1:
// 				meeting_id: 5
// 				entitled_group_ids: [30, 31]
// 				pollmethod: Y
// 				global_yes: true
// 				backend: fast
// 				type: pseudoanonymous
// 				content_object_id: some_field/1
// 				sequential_number: 1
// 				onehundred_percent_base: base
// 				title: myPoll

// 			group/30/meeting_user_ids: [500]
// 			group/31/meeting_user_ids: [510]

// 			user:
// 				50:
// 					is_present_in_meeting_ids: [5]

// 				51:
// 					is_present_in_meeting_ids: [5]

// 				52:
// 					is_present_in_meeting_ids: [5]

// 				53:
// 					is_present_in_meeting_ids: [5]

// 			meeting_user:
// 				500:
// 					user_id: 50
// 					vote_delegated_to_id: 520
// 					meeting_id: 5
// 				510:
// 					user_id: 51
// 					vote_delegated_to_id: 530
// 					meeting_id: 5
// 				520:
// 					user_id: 52
// 					meeting_id: 5
// 				530:
// 					user_id: 53
// 					meeting_id: 5
// 			`,
// 			4,
// 		},
// 	} {
// 		t.Run(tt.name, func(t *testing.T) {
// 			dsCount := dsmock.NewCounter(dsmock.Stub(dsmock.YAMLData(tt.data)))
// 			ds := dsmock.NewCache(dsCount)
// 			fetcher := dsmodels.New(ds)

// 			poll, err := fetcher.Poll(1).First(ctx)
// 			if err != nil {
// 				t.Fatalf("loadPoll returned: %v", err)
// 			}

// 			dsCount.(*dsmock.Counter).Reset()

// 			if err := preload(ctx, dsfetch.New(ds), poll); err != nil {
// 				t.Errorf("preload returned: %v", err)
// 			}

// 			if got := dsCount.(*dsmock.Counter).Count(); got != tt.expectCount {
// 				buf := new(bytes.Buffer)
// 				for _, req := range dsCount.(*dsmock.Counter).Requests() {
// 					fmt.Fprintln(buf, req)
// 				}
// 				t.Errorf("preload send %d requests, expected %d:\n%s", got, tt.expectCount, buf)
// 			}
// 		})
// 	}
// }
