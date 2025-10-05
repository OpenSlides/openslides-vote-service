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
			name:        "Approval: Vote Yes",
			method:      "approval",
			config:      "",
			vote:        `"Yes"`,
			expectValid: true,
		},
		{
			name:        "Approval: unknown string",
			method:      "approval",
			config:      "",
			vote:        `"Y"`,
			expectValid: false,
		},
		{
			name:        "Approval: Abstain",
			method:      "approval",
			config:      "",
			vote:        `"Abstain"`,
			expectValid: true,
		},
		{
			name:        "Approval: Abstain deactivated",
			method:      "approval",
			config:      `{"allow_abstain": false}`,
			vote:        `"Abstain"`,
			expectValid: false,
		},
		{
			name:        "Selection invalid json",
			method:      "selection",
			config:      `{"options":{"1":"Max","2":"Hubert"}}`,
			vote:        `[0`,
			expectValid: false,
		},
		{
			name:        "Selection",
			method:      "selection",
			config:      `{"options":{"1":"Max","2":"Hubert"}}`,
			vote:        `["1"]`,
			expectValid: true,
		},
		{
			name:        "Selection same value multiple times",
			method:      "selection",
			config:      `{"options":{"1":"Max","2":"Hubert"}}`,
			vote:        `["1","1"]`,
			expectValid: false,
		},
		{
			name:        "Selection unknown key",
			method:      "selection",
			config:      `{"options":{"1":"Max","2":"Hubert"}}`,
			vote:        `["unknown"]`,
			expectValid: false,
		},
		{
			name:        "Selection max_options_amount",
			method:      "selection",
			config:      `{"options":{"1":"Max","2":"Hubert"},"max_options_amount":1}`,
			vote:        `["1"]`,
			expectValid: true,
		},
		{
			name:        "Selection max_options_amount too many",
			method:      "selection",
			config:      `{"options":{"1":"Max","2":"Hubert"},"max_options_amount":1}`,
			vote:        `["1","2"]`,
			expectValid: false,
		},
		{
			name:        "Selection min_options_amount",
			method:      "selection",
			config:      `{"options":{"1":"Max","2":"Hubert"},"min_options_amount":1}`,
			vote:        `["1"]`,
			expectValid: true,
		},
		{
			name:        "Selection min_options_amount too few",
			method:      "selection",
			config:      `{"options":{"1":"Max","2":"Hubert"},"min_options_amount":2}`,
			vote:        `["1"]`,
			expectValid: false,
		},
		{
			name:        "Selection nota",
			method:      "selection",
			config:      `{"options":{"1":"Max","2":"Hubert"},"min_options_amount":2,"allow_nota":true}`,
			vote:        `"nota"`,
			expectValid: true,
		},
		{
			name:        "Rating-Score",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"}}`,
			vote:        `{"1":3}`,
			expectValid: true,
		},
		{
			name:        "Rating-Score invalid key",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"}}`,
			vote:        `{"0":3}`,
			expectValid: false,
		},
		{
			name:        "Rating-Score with negative value",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"}}`,
			vote:        `{"1":-3}`,
			expectValid: false,
		},
		{
			name:        "Rating-Score max_options_amount",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"max_options_amount":1}`,
			vote:        `{"1":3}`,
			expectValid: true,
		},
		{
			name:        "Rating-Score max_options_amount too many",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"max_options_amount":1}`,
			vote:        `{"1":3, "2":1}`,
			expectValid: false,
		},
		{
			name:        "Rating-Score min_options_amount",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"min_options_amount":1}`,
			vote:        `{"1":3}`,
			expectValid: true,
		},
		{
			name:        "Rating-Score min_options_amount too few",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"min_options_amount":2}`,
			vote:        `{"1":3}`,
			expectValid: false,
		},
		{
			name:        "Rating-Score max_votes_per_option",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"max_votes_per_option":2}`,
			vote:        `{"1":2}`,
			expectValid: true,
		},
		{
			name:        "Rating-Score max_votes_per_option too many",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"max_votes_per_option":2}`,
			vote:        `{"1":3}`,
			expectValid: false,
		},
		{
			name:        "Rating-Score max_vote_sum",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"max_vote_sum":5}`,
			vote:        `{"1":3}`,
			expectValid: true,
		},
		{
			name:        "Rating-Score max_vote_sum too many",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"max_vote_sum":5}`,
			vote:        `{"1":6}`,
			expectValid: false,
		},
		{
			name:        "Rating-Score max_vote_sum too many on different options",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"max_vote_sum":5}`,
			vote:        `{"1":3, "2":3}`,
			expectValid: false,
		},
		{
			name:        "Rating-Score min_vote_sum on one vote",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"min_vote_sum":10}`,
			vote:        `{"1":5}`,
			expectValid: false,
		},
		{
			name:        "Rating-Score min_vote_sum on many votes",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"min_vote_sum":10}`,
			vote:        `{"1":5, "2":4}`,
			expectValid: false,
		},
		{
			name:        "Rating-Score min_vote_sum enough",
			method:      "rating-score",
			config:      `{"options":{"1":"Max","2":"Hubert"},"min_vote_sum":1}`,
			vote:        `{"1":5, "2":5}`,
			expectValid: true,
		},
		{
			name:        "Rating-Approval",
			method:      "rating-approval",
			config:      `{"options":{"1":"Max","2":"Hubert"}}`,
			vote:        `{"1":"Yes", "2":"No"}`,
			expectValid: true,
		},
		{
			name:        "Rating-Approval invalid key",
			method:      "rating-approval",
			config:      `{"options":{"1":"Max","2":"Hubert"}}`,
			vote:        `{"0":"Yes", "2":"No"}`,
			expectValid: false,
		},
		{
			name:        "Rating-Approval disallow abstain",
			method:      "rating-approval",
			config:      `{"options":{"1":"Max","2":"Hubert"},"allow_abstain":false}`,
			vote:        `{"1":"Yes", "2":"Abstain"}`,
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
			name:   "Approval: Vote Yes",
			method: "approval",
			config: "",
			votes: []dsmodels.Vote{
				{Value: `"Yes"`},
				{Value: `"Yes"`},
				{Value: `"No"`},
			},
			expectResult: `{"no":"1","yes":"2"}`,
		},
		{
			name:   "Approval with invalid",
			method: "approval",
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
			config: `{"options":{"tom":"Tom","gregor":"Gregor","hans":"Hans"}}`,
			votes: []dsmodels.Vote{
				{Value: `["tom","gregor"]`},
				{Value: `["gregor","hans"]`},
				{Value: `["hans"]`, Weight: "5"},
			},
			expectResult: `{"gregor":"2","hans":"6","tom":"1"}`,
		},
		{
			name:   "Selection abstain",
			method: "selection",
			config: `{"options":{"tom":"Tom","gregor":"Gregor","hans":"Hans"}}`,
			votes: []dsmodels.Vote{
				{Value: `["tom","gregor"]`},
				{Value: `[]`},
				{Value: `[]`, Weight: "5"},
			},
			expectResult: `{"abstain":"6","gregor":"1","tom":"1"}`,
		},
		{
			name:   "Selection nota",
			method: "selection",
			config: `{"options":{"tom":"Tom","gregor":"Gregor","hans":"Hans"},"allow_nota":true}`,
			votes: []dsmodels.Vote{
				{Value: `["tom","gregor"]`},
				{Value: `"nota"`},
				{Value: `"nota"`, Weight: "5"},
			},
			expectResult: `{"gregor":"1","nota":"6","tom":"1"}`,
		},
		{
			name:   "Rating-Score",
			method: "rating-score",
			config: `{"options":{"tom":"Tom","gregor":"Gregor","hans":"Hans"}}`,
			votes: []dsmodels.Vote{
				{Value: `{"tom":3,"gregor":3}`},
				{Value: `{"gregor":2,"hans":3}`},
				{Value: `{"hans":5}`, Weight: "5"},
			},
			expectResult: `{"gregor":"5","hans":"28","tom":"3"}`,
		},
		{
			name:   "Rating-Score Abstain",
			method: "rating-score",
			config: `{"options":{"tom":"Tom","gregor":"Gregor","hans":"Hans"}}`,
			votes: []dsmodels.Vote{
				{Value: `{"tom":3,"gregor":3}`},
				{Value: `{}`},
				{Value: `{}`, Weight: "5"},
			},
			expectResult: `{"abstain":"6","gregor":"3","tom":"3"}`,
		},
		{
			name:   "Rating-Approval",
			method: "rating-approval",
			config: `{"options":{"tom":"Tom","gregor":"Gregor","hans":"Hans"}}`,
			votes: []dsmodels.Vote{
				{Value: `{"tom":"yes","gregor":"no"}`},
				{Value: `{"gregor":"yes","hans":"no"}`},
				{Value: `{"hans":"yes"}`, Weight: "5"},
			},
			expectResult: `{"gregor":{"no":"1","yes":"1"},"hans":{"no":"1","yes":"5"},"tom":{"yes":"1"}}`,
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
