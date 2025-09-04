package vote_test

import (
	"encoding/json"
	"testing"

	"github.com/OpenSlides/openslides-go/datastore/flow"
	"github.com/OpenSlides/openslides-go/datastore/pgtest"
	"github.com/OpenSlides/openslides-vote-service/vote"
)

func TestValidateVote(t *testing.T) {
	for _, tt := range []struct {
		name        string
		method      string
		config      string
		vote        string
		expectValid bool
	}{
		{
			name:        "Unknown Method",
			method:      "unknown",
			config:      "",
			vote:        "",
			expectValid: false,
		},
		{
			name:        "Motion: Vote Yes",
			method:      "motion",
			config:      "",
			vote:        `"Yes"`,
			expectValid: true,
		},
		{
			name:        "Motion: unknown string",
			method:      "motion",
			config:      "",
			vote:        `"Y"`,
			expectValid: false,
		},
		{
			name:        "Motion: Abstain",
			method:      "motion",
			config:      "",
			vote:        `"Abstain"`,
			expectValid: true,
		},
		{
			name:        "Motion: Abstain deactivated",
			method:      "motion",
			config:      `{"abstain": false}`,
			vote:        `"Abstain"`,
			expectValid: false,
		},
		{
			name:        "Selection",
			method:      "selection",
			config:      `{"options":["Max","Hubert"]}`,
			vote:        `[0]`,
			expectValid: true,
		},
		{
			name:        "Selection no low",
			method:      "selection",
			config:      `{"options":["Max","Hubert"]}`,
			vote:        `[-1]`,
			expectValid: false,
		},
		{
			name:        "Selection too high",
			method:      "selection",
			config:      `{"options":["Max","Hubert"]}`,
			vote:        `[2]`,
			expectValid: false,
		},
		{
			name:        "Selection not a number",
			method:      "selection",
			config:      `{"options":["Max","Hubert"]}`,
			vote:        `["two"]`,
			expectValid: false,
		},
		{
			name:        "Selection max_options_amount",
			method:      "selection",
			config:      `{"options":["Max","Hubert"],"max_options_amount":1}`,
			vote:        `[0]`,
			expectValid: true,
		},
		{
			name:        "Selection max_options_amount too many",
			method:      "selection",
			config:      `{"options":["Max","Hubert"],"max_options_amount":1}`,
			vote:        `[0,1]`,
			expectValid: false,
		},
		{
			name:        "Selection min_options_amount",
			method:      "selection",
			config:      `{"options":["Max","Hubert"],"min_options_amount":1}`,
			vote:        `[0]`,
			expectValid: true,
		},
		{
			name:        "Selection min_options_amount too few",
			method:      "selection",
			config:      `{"options":["Max","Hubert"],"min_options_amount":2}`,
			vote:        `[0]`,
			expectValid: false,
		},
		{
			name:        "Rating",
			method:      "rating",
			config:      `{"options":["Max","Hubert"]}`,
			vote:        `{"1":3}`,
			expectValid: true,
		},
		{
			name:        "Rating with negative value",
			method:      "rating",
			config:      `{"options":["Max","Hubert"]}`,
			vote:        `{"1":-3}`,
			expectValid: false,
		},
		{
			name:        "Rating max_options_amount",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"max_options_amount":1}`,
			vote:        `{"1":3}`,
			expectValid: true,
		},
		{
			name:        "Rating max_options_amount too many",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"max_options_amount":1}`,
			vote:        `{"1":3, "0":1}`,
			expectValid: false,
		},
		{
			name:        "Rating min_options_amount",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"min_options_amount":1}`,
			vote:        `{"1":3}`,
			expectValid: true,
		},
		{
			name:        "Rating min_options_amount too few",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"min_options_amount":2}`,
			vote:        `{"1":3}`,
			expectValid: false,
		},
		{
			name:        "Rating max_votes_per_option",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"max_votes_per_option":2}`,
			vote:        `{"1":2}`,
			expectValid: true,
		},
		{
			name:        "Rating max_votes_per_option too many",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"max_votes_per_option":2}`,
			vote:        `{"1":3}`,
			expectValid: false,
		},
		{
			name:        "Rating max_vote_sum",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"max_vote_sum":5}`,
			vote:        `{"1":3}`,
			expectValid: true,
		},
		{
			name:        "Rating max_vote_sum too many",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"max_vote_sum":5}`,
			vote:        `{"1":6}`,
			expectValid: false,
		},
		{
			name:        "Rating max_vote_sum too many on different options",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"max_vote_sum":5}`,
			vote:        `{"1":3, "2":3}`,
			expectValid: false,
		},
		{
			name:        "Rating min_vote_sum on one vote",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"min_vote_sum":10}`,
			vote:        `{"1":5}`,
			expectValid: false,
		},
		{
			name:        "Rating min_vote_sum on many votes",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"min_vote_sum":10}`,
			vote:        `{"1":5, "2":4}`,
			expectValid: false,
		},
		{
			name:        "Rating min_vote_sum enough",
			method:      "rating",
			config:      `{"options":["Max","Hubert"],"min_vote_sum":10}`,
			vote:        `{"1":5, "0":5}`,
			expectValid: true,
		},

		// // Test Method YN and YNA
		// {
		// 	"Method YN, Global Y, Vote Y",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YN",
		// 		GlobalYes:  true,
		// 	},
		// 	`"Y"`,
		// 	true,
		// },
		// {
		// 	"Method YN, Not Global Y, Vote Y",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YN",
		// 		GlobalYes:  false,
		// 	},
		// 	`"Y"`,
		// 	false,
		// },
		// {
		// 	"Method YNA, Global N, Vote N",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YNA",
		// 		GlobalNo:   true,
		// 	},
		// 	`"N"`,
		// 	true,
		// },
		// {
		// 	"Method YNA, Not Global N, Vote N",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YNA",
		// 		GlobalYes:  false,
		// 	},
		// 	`"N"`,
		// 	false,
		// },
		// {
		// 	"Method YNA, Y on Option",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YNA",
		// 		OptionIDs:  []int{1, 2},
		// 	},
		// 	`{"1":"Y"}`,
		// 	true,
		// },
		// {
		// 	"Method YNA, N on Option",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YNA",
		// 		OptionIDs:  []int{1, 2},
		// 	},
		// 	`{"1":"N"}`,
		// 	true,
		// },
		// {
		// 	"Method YNA, A on Option",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YNA",
		// 		OptionIDs:  []int{1, 2},
		// 	},
		// 	`{"1":"A"}`,
		// 	true,
		// },
		// {
		// 	"Method YN, A on Option",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YN",
		// 		OptionIDs:  []int{1, 2},
		// 	},
		// 	`{"1":"A"}`,
		// 	false,
		// },
		// {
		// 	"Method YN, Y on wrong Option",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YN",
		// 		OptionIDs:  []int{1, 2},
		// 	},
		// 	`{"3":"Y"}`,
		// 	false,
		// },
		// {
		// 	"Method YNA, Vote on many Options",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YNA",
		// 		OptionIDs:  []int{1, 2, 3},
		// 	},
		// 	`{"1":"Y","2":"N","3":"A"}`,
		// 	true,
		// },
		// {
		// 	"Method YNA, Amount on Option",
		// 	dsmodels.Poll{
		// 		Pollmethod: "YNA",
		// 		OptionIDs:  []int{1, 2, 3},
		// 	},
		// 	`{"1":1}`,
		// 	false,
		// },
		// {
		// 	"Method YNA with to low selected",
		// 	dsmodels.Poll{
		// 		Pollmethod:     "YNA",
		// 		OptionIDs:      []int{1, 2, 3},
		// 		MinVotesAmount: 2,
		// 	},
		// 	`{"1":"Y"}`,
		// 	false,
		// },
		// {
		// 	"Method YNA with enough selected",
		// 	dsmodels.Poll{
		// 		Pollmethod:     "YNA",
		// 		OptionIDs:      []int{1, 2, 3},
		// 		MinVotesAmount: 2,
		// 	},
		// 	`{"1":"Y", "2":"N"}`,
		// 	true,
		// },
		// {
		// 	"Method YNA with to many selected",
		// 	dsmodels.Poll{
		// 		Pollmethod:     "YNA",
		// 		OptionIDs:      []int{1, 2, 3},
		// 		MaxVotesAmount: 2,
		// 	},
		// 	`{"1":"Y", "2":"N", "3":"A"}`,
		// 	false,
		// },
		// {
		// 	"Method YNA with not to many selected",
		// 	dsmodels.Poll{
		// 		Pollmethod:     "YNA",
		// 		OptionIDs:      []int{1, 2, 3},
		// 		MaxVotesAmount: 2,
		// 	},
		// 	`{"1":"Y", "2":"N"}`,
		// 	true,
		// },

		// // Unknown method
		// {
		// 	"Method Unknown",
		// 	dsmodels.Poll{
		// 		Pollmethod: "XXX",
		// 	},
		// 	`"Y"`,
		// 	false,
		// },
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := json.RawMessage(tt.config)
			if tt.config == "" {
				cfg = nil
			}
			err := vote.ValidateVote(tt.method, cfg, json.RawMessage(tt.vote))

			if tt.expectValid {
				if err != nil {
					t.Fatalf("Validate returned unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("Got no validation error")
			}
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
