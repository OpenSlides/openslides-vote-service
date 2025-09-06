package vote_test

import (
	"encoding/json"
	"testing"

	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/OpenSlides/openslides-vote-service/vote"
)

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
			expectResult: `{"yes":"2","no":"1"}`,
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
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := json.RawMessage(tt.config)
			if tt.config == "" {
				cfg = nil
			}
			result, err := vote.CreateResult(tt.method, cfg, tt.votes)
			if err != nil {
				t.Fatalf("CreateResult: %v", err)
			}

			if string(result) != tt.expectResult {
				t.Errorf("Got: %s, expected %s", result, tt.expectResult)
			}
		})
	}
}
