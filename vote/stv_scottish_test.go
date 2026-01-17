package vote_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/OpenSlides/openslides-vote-service/vote"
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
			config:      `{"options": [1,2,3,4,5], "posts": 3}`,
			vote:        `[2,3,4,5,1]`,
			expectValid: true,
		},
		{
			name:        "STV Scottish: partial vote valid",
			method:      "stv_scottish",
			config:      `{"options": [1,2,3,4,5], "posts": 3}`,
			vote:        `[2,3,4]`,
			expectValid: true,
		},
		{
			name:        "STV Scottish: empty vote which is valid",
			method:      "stv_scottish",
			config:      `{"options": [1,2,3,4,5], "posts": 3}`,
			vote:        `[]`,
			expectValid: true,
		},
		{
			name:        "STV Scottish: wrong options",
			method:      "stv_scottish",
			config:      `{"options": [1,2,3,4,5], "posts": 3}`,
			vote:        `[2,3,4,5,6]`,
			expectValid: false,
		},
		{
			name:        "STV Scottish: double option",
			method:      "stv_scottish",
			config:      `{"options": [1,2,3,4,5], "posts": 3}`,
			vote:        `[2,3,4,5,2]`,
			expectValid: false,
		},
		{
			name:        "STV Scottish: bad payload with string",
			method:      "stv_scottish",
			config:      `{"options": [1,2,3,4,5], "posts": 3}`,
			vote:        `["here an invalid string instead of an integer", 1]`,
			expectValid: false,
		},
		{
			name:        "STV Scottish: bad payload with string as number",
			method:      "stv_scottish",
			config:      `{"options": [1,2,3,4,5], "posts": 3}`,
			vote:        `["2", 1]`,
			expectValid: false,
		},
		{
			name:        "STV Scottish: bad payload with invalid JSON",
			method:      "stv_scottish",
			config:      `{"options": [1,2,3,4,5], "posts": 3}`,
			vote:        `[1`,
			expectValid: false,
		},
		{
			name:        "STV Scottish: empty payload",
			method:      "stv_scottish",
			config:      `{"options": [1,2,3,4,5], "posts": 3}`,
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
