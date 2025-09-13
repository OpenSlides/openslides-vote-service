package vote_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
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
			config:      `{"allow_abstain": false}`,
			vote:        `"Abstain"`,
			expectValid: false,
		},
		{
			name:        "Selection invalid json",
			method:      "selection",
			config:      `{"options":["Max","Hubert"]}`,
			vote:        `[0`,
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
			name:        "Selection same value multiple times",
			method:      "selection",
			config:      `{"options":["Max","Hubert"]}`,
			vote:        `[0,0]`,
			expectValid: false,
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
			name:        "Selection nota",
			method:      "selection",
			config:      `{"options":["Max","Hubert"],"min_options_amount":2,"allow_nota":true}`,
			vote:        `"nota"`,
			expectValid: true,
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
		{
			name:        "Rating-Motion",
			method:      "rating-motion",
			config:      `{"options":["Max","Hubert"]}`,
			vote:        `{"0":"Yes", "1":"No"}`,
			expectValid: true,
		},
		{
			name:        "Rating-Motion disallow abstain",
			method:      "rating-motion",
			config:      `{"options":["Max","Hubert"],"allow_abstain":false}`,
			vote:        `{"0":"Yes", "1":"Abstain"}`,
			expectValid: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := vote.ValidateVote(tt.method, tt.config, json.RawMessage(tt.vote))

			if err != nil {
				if !errors.Is(err, vote.ErrInvalid) {
					t.Errorf("Got unexpected error: %v", err)
				}
			}

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

func TestCreateResult(t *testing.T) {
	for _, tt := range []struct {
		name         string
		method       string
		config       string
		votes        []dsmodels.Vote
		expectResult string
	}{
		{
			name:   "Motion: Vote Yes",
			method: "motion",
			config: "",
			votes: []dsmodels.Vote{
				{Value: `"Yes"`},
				{Value: `"Yes"`},
				{Value: `"No"`},
			},
			expectResult: `{"no":"1","yes":"2"}`,
		},
		{
			name:   "Motion with invalid",
			method: "motion",
			config: "",
			votes: []dsmodels.Vote{
				{Value: `"Yes"`},
				{Value: `"Yes"`},
				{Value: `"No"`},
				{Value: `"ABC"`},
			},
			expectResult: `{"invalid":1,"no":"1","yes":"2"}`,
		},
		{
			name:   "Selection",
			method: "selection",
			config: `{"options":["tom","gregor","hans"]}`,
			votes: []dsmodels.Vote{
				{Value: `[0,1]`},
				{Value: `[1,2]`},
				{Value: `[2]`, Weight: "5"},
			},
			expectResult: `{"0":"1","1":"2","2":"6"}`,
		},
		{
			name:   "Selection abstain",
			method: "selection",
			config: `{"options":["tom","gregor","hans"]}`,
			votes: []dsmodels.Vote{
				{Value: `[0,1]`},
				{Value: `[]`},
				{Value: `[]`, Weight: "5"},
			},
			expectResult: `{"0":"1","1":"1","abstain":"6"}`,
		},
		{
			name:   "Selection nota",
			method: "selection",
			config: `{"options":["tom","gregor","hans"],"allow_nota":true}`,
			votes: []dsmodels.Vote{
				{Value: `[0,1]`},
				{Value: `"nota"`},
				{Value: `"nota"`, Weight: "5"},
			},
			expectResult: `{"0":"1","1":"1","nota":"6"}`,
		},
		{
			name:   "Rating",
			method: "rating",
			config: `{"options":["tom","gregor","hans"]}`,
			votes: []dsmodels.Vote{
				{Value: `{"0":3,"1":3}`},
				{Value: `{"1":2,"2":3}`},
				{Value: `{"2":5}`, Weight: "5"},
			},
			expectResult: `{"0":"3","1":"5","2":"28"}`,
		},
		{
			name:   "Rating Abstain",
			method: "rating",
			config: `{"options":["tom","gregor","hans"]}`,
			votes: []dsmodels.Vote{
				{Value: `{"0":3,"1":3}`},
				{Value: `{}`},
				{Value: `{}`, Weight: "5"},
			},
			expectResult: `{"0":"3","1":"3","abstain":"6"}`,
		},
		{
			name:   "Rating-Motion",
			method: "rating-motion",
			config: `{"options":["tom","gregor","hans"]}`,
			votes: []dsmodels.Vote{
				{Value: `{"0":"yes","1":"no"}`},
				{Value: `{"1":"yes","2":"no"}`},
				{Value: `{"2":"yes"}`, Weight: "5"},
			},
			expectResult: `{"0":{"yes":"1"},"1":{"no":"1","yes":"1"},"2":{"no":"1","yes":"5"}}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {

			result, err := vote.CreateResult(tt.method, tt.config, tt.votes)
			if err != nil {
				t.Fatalf("CreateResult: %v", err)
			}

			if string(result) != tt.expectResult {
				t.Errorf("Got: %s, expected %s", result, tt.expectResult)
			}
		})
	}
}
