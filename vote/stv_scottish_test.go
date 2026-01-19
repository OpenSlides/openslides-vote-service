package vote_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/OpenSlides/openslides-vote-service/vote"
	"github.com/shopspring/decimal"
)

func TestValidateVoteScottischSTV(t *testing.T) {
	for _, tt := range []struct {
		name        string
		method      string
		config      string
		vote        string
		expectValid bool
	}{
		{
			name:        "STV Scottish: full vote valid",
			method:      "stv_scottish",
			config:      `{"options": [1, 2, 3, 4, 5], "posts": 3}`,
			vote:        `[2, 3, 4, 5, 1]`,
			expectValid: true,
		},
		{
			name:        "STV Scottish: partial vote valid",
			method:      "stv_scottish",
			config:      `{"options": [1, 2, 3, 4, 5], "posts": 3}`,
			vote:        `[2, 3, 4]`,
			expectValid: true,
		},
		{
			name:        "STV Scottish: empty vote which is valid",
			method:      "stv_scottish",
			config:      `{"options": [1, 2, 3, 4, 5], "posts": 3}`,
			vote:        `[]`,
			expectValid: true,
		},
		{
			name:        "STV Scottish: wrong options",
			method:      "stv_scottish",
			config:      `{"options": [1, 2, 3, 4, 5], "posts": 3}`,
			vote:        `[2, 3, 4, 5, 6]`,
			expectValid: false,
		},
		{
			name:        "STV Scottish: double option",
			method:      "stv_scottish",
			config:      `{"options": [1, 2, 3, 4, 5], "posts": 3}`,
			vote:        `[2, 3, 4, 5, 2]`,
			expectValid: false,
		},
		{
			name:        "STV Scottish: bad payload with string",
			method:      "stv_scottish",
			config:      `{"options": [1, 2, 3, 4, 5], "posts": 3}`,
			vote:        `["here a string", 1]`,
			expectValid: false,
		},
		{
			name:        "STV Scottish: bad payload with number as string",
			method:      "stv_scottish",
			config:      `{"options": [1, 2, 3, 4, 5], "posts": 3}`,
			vote:        `["2", 1]`,
			expectValid: false,
		},
		{
			name:        "STV Scottish: bad payload with invalid JSON",
			method:      "stv_scottish",
			config:      `{"options": [1, 2, 3, 4, 5], "posts": 3}`,
			vote:        `[1`,
			expectValid: false,
		},
		{
			name:        "STV Scottish: empty payload",
			method:      "stv_scottish",
			config:      `{"options": [1, 2, 3, 4, 5], "posts": 3}`,
			vote:        ``,
			expectValid: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := vote.ValidateBallot(tt.method, tt.config, json.RawMessage(tt.vote))

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

// Code copied

type resultSTVScottish struct {
	Invalid int             `json:"invalid"`
	Quota   decimal.Decimal `json:"quota"`
	Elected []int           `json:"elected"`
	Stages  []stage         `json:"stages"`
}

type stage map[int]optionResult

type optionResult struct {
	Votes  decimal.Decimal `json:"votes"`
	Status string          `json:"status"`
}

// End of copy

func TestCreateResultScottischSTV(t *testing.T) {
	for _, tt := range []struct {
		name           string
		method         string
		config         string
		votes          []dsmodels.Ballot
		expectedResult resultSTVScottish
	}{
		{
			name:   "STV Scottish",
			method: "stv_scottish",
			config: `{"posts": 2, "options": [1, 2, 3]}`,
			votes: []dsmodels.Ballot{
				{Value: `[1,2,3]`},
				{Value: `[1,2,3]`},
				{Value: `[1,2,3]`},
				{Value: `[1,2,3]`},
				{Value: `[1,2,3]`},
				{Value: `[1,3,2]`},
				{Value: `[1,3,2]`},
				{Value: `[1,3,2]`},
				{Value: `[2,1,3]`},
				{Value: `[2,3,1]`},
				{Value: `[3,1,2]`},
				{Value: `[3,2,1]`},
				{Value: `[]`},
			},
			expectedResult: resultSTVScottish{
				Invalid: 1,
				Quota:   decimal.NewFromInt(5),
				Elected: []int{1, 2},
				Stages: []stage{
					stage{
						1: optionResult{Votes: decimal.NewFromInt(8), Status: "elected"},
						2: optionResult{Votes: decimal.NewFromInt(2), Status: "continuing"},
						3: optionResult{Votes: decimal.NewFromInt(2), Status: "continuing"},
					},
					stage{
						1: optionResult{Votes: decimal.NewFromInt(5), Status: "elected"},
						2: optionResult{Votes: decimal.NewFromFloat(3.875), Status: "continuing"},
						3: optionResult{Votes: decimal.NewFromFloat(3.125), Status: "excluded"},
					},
					stage{
						1: optionResult{Votes: decimal.NewFromInt(5), Status: "elected"},
						2: optionResult{Votes: decimal.NewFromFloat(3.875), Status: "elected"},
						3: optionResult{Votes: decimal.NewFromInt(0), Status: "excluded"},
					},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result, err := vote.CreateResult(tt.method, tt.config, false, tt.votes)
			if err != nil {
				t.Fatalf("CreateResult: %v", err)
			}

			var got resultSTVScottish
			if err := json.Unmarshal([]byte(result), &got); err != nil {
				t.Errorf("Unmarshalling JSON: %v", err)
			}
			if tt.expectedResult.Invalid != got.Invalid {
				t.Errorf("Wrong result: field invalid: expected %d, got %d", tt.expectedResult.Invalid, got.Invalid)
			}
			if !tt.expectedResult.Quota.Equal(got.Quota) {
				t.Errorf("Wrong result: field quota: expected %v, got %v", tt.expectedResult.Quota, got.Quota)
			}
			if !slices.Equal(tt.expectedResult.Elected, got.Elected) {
				t.Errorf("Wrong result: field elected: expected %v, got %v", tt.expectedResult.Elected, got.Elected)
			}
			if len(tt.expectedResult.Stages) != len(got.Stages) {
				t.Fatalf("Wrong result: field stages: expected len %d, got %d", len(tt.expectedResult.Stages), len(got.Stages))
			}
			for i, s := range tt.expectedResult.Stages {
				for k, v := range s {
					if v.Status != got.Stages[i][k].Status || !v.Votes.Equal(got.Stages[i][k].Votes) {
						t.Errorf("Wrong result: field stages, stage with index %d at key %d: expected %v, got %v", i, k, v, got.Stages[i][k])
					}
				}
			}
		})
	}
}
