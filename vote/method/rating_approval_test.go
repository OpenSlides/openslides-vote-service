package method_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/OpenSlides/openslides-vote-service/vote/method"
	"github.com/shopspring/decimal"
)

func TestRatingApprovalValidateVote(t *testing.T) {
	for _, tt := range []struct {
		name        string
		method      string
		config      string
		vote        string
		expectValid bool
	}{
		{
			name:        "Rating Approval",
			method:      "rating_approval",
			config:      `{"options":[1,2]}`,
			vote:        `{"1":"Yes", "2":"No"}`,
			expectValid: true,
		},
		{
			name:        "Rating Approval invalid key",
			method:      "rating_approval",
			config:      `{"options":[1,2]}`,
			vote:        `{"0":"Yes", "2":"No"}`,
			expectValid: false,
		},
		{
			name:        "Rating Approval disallow abstain",
			method:      "rating_approval",
			config:      `{"options":[1,2],"allow_abstain":false}`,
			vote:        `{"1":"Yes", "2":"Abstain"}`,
			expectValid: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, err := method.RatingApprovalFromRequest([]byte(tt.config))
			if err != nil {
				t.Fatalf("Error: %v", err)
			}

			err = a.ValidateBallot(json.RawMessage(tt.vote))

			if err != nil {
				if _, ok := errors.AsType[method.InvalidBallotError](err); !ok {
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

func TestRatingApprovalCreateResult(t *testing.T) {
	for _, tt := range []struct {
		name         string
		method       string
		config       string
		ballots      []dsmodels.Ballot
		expectResult string
	}{
		{
			name:   "Rating Approval",
			method: "rating_approval",
			config: `{"options":[1,2,3]}`,
			ballots: []dsmodels.Ballot{
				{Value: `{"1":"yes","2":"no"}`},
				{Value: `{"2":"yes","3":"no"}`},
				{Value: `{"3":"yes"}`, Weight: decimal.NewFromInt(5)},
			},
			expectResult: `{"1":{"yes":"1"},"2":{"no":"1","yes":"1"},"3":{"no":"1","yes":"5"}}`,
		},
		{
			name:   "Rating Approval with out abstain but with invalid",
			method: "rating_approval",
			config: `{"options":[1,2,3],"allow_abstain":false}`,
			ballots: []dsmodels.Ballot{
				{Value: `{"1":"yes","2":"abstain"}`},
				{Value: `{"1":"yes","2":"no"}`},
			},
			expectResult: `{"1":{"yes":"1"},"2":{"no":"1"},"invalid":1}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, err := method.RatingApprovalFromRequest([]byte(tt.config))
			if err != nil {
				t.Fatalf("Error: %v", err)
			}

			result, err := a.Result(tt.ballots)
			if err != nil {
				t.Fatalf("CreateResult: %v", err)
			}

			if string(result) != tt.expectResult {
				t.Errorf("Got: %s, expected %s", result, tt.expectResult)
			}
		})
	}
}
