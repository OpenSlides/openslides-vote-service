package vote_test

import (
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
