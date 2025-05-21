package vote

import (
	"encoding/json"
	"testing"

	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
)

func TestVoteValidate(t *testing.T) {
	for _, tt := range []struct {
		name        string
		poll        dsmodels.Poll
		vote        string
		expectValid bool
	}{
		// Test Method Y and N.
		{
			"Method Y, Global Y, Vote Y",
			dsmodels.Poll{
				Pollmethod: "Y",
				GlobalYes:  true,
			},
			`"Y"`,
			true,
		},
		{
			"Method Y, Vote Y",
			dsmodels.Poll{
				Pollmethod: "Y",
				GlobalYes:  false,
			},
			`"Y"`,
			false,
		},
		{
			"Method Y, Vote N",
			dsmodels.Poll{
				Pollmethod: "Y",
				GlobalNo:   false,
			},
			`"N"`,
			false,
		},
		{
			// The poll config is invalid. A poll with method Y should not allow global_no.
			"Method Y, Global N, Vote N",
			dsmodels.Poll{
				Pollmethod: "Y",
				GlobalNo:   true,
			},
			`"N"`,
			true,
		},
		{
			"Method N, Global N, Vote N",
			dsmodels.Poll{
				Pollmethod: "N",
				GlobalNo:   true,
			},
			`"N"`,
			true,
		},
		{
			"Method Y, Vote Option",
			dsmodels.Poll{
				Pollmethod: "Y",
				OptionIDs:  []int{1, 2},
			},
			`{"1":1}`,
			true,
		},
		{
			"Method Y, Vote on to many Options",
			dsmodels.Poll{
				Pollmethod: "Y",
				OptionIDs:  []int{1, 2},
			},
			`{"1":1,"2":1}`,
			false,
		},
		{
			"Method Y, Vote on one option with to high amount",
			dsmodels.Poll{
				Pollmethod: "Y",
				OptionIDs:  []int{1, 2},
			},
			`{"1":5}`,
			false,
		},
		{
			"Method Y, Vote on many option with to high amount",
			dsmodels.Poll{
				Pollmethod:        "Y",
				OptionIDs:         []int{1, 2},
				MaxVotesAmount:    2,
				MaxVotesPerOption: 1,
			},
			`{"1":1,"2":2}`,
			false,
		},
		{
			"Method Y, Vote on one option with correct amount",
			dsmodels.Poll{
				Pollmethod:        "Y",
				OptionIDs:         []int{1, 2},
				MaxVotesAmount:    5,
				MaxVotesPerOption: 7,
			},
			`{"1":5}`,
			true,
		},
		{
			"Method Y, Vote on one option with to less amount",
			dsmodels.Poll{
				Pollmethod:        "Y",
				OptionIDs:         []int{1, 2},
				MinVotesAmount:    10,
				MaxVotesAmount:    10,
				MaxVotesPerOption: 10,
			},
			`{"1":5}`,
			false,
		},
		{
			"Method Y, Vote on many options with to less amount",
			dsmodels.Poll{
				Pollmethod:     "Y",
				OptionIDs:      []int{1, 2},
				MinVotesAmount: 10,
			},
			`{"1":1,"2":1}`,
			false,
		},
		{
			"Method Y, Vote on one option with -1 amount",
			dsmodels.Poll{
				Pollmethod: "Y",
				OptionIDs:  []int{1, 2},
			},
			`{"1":-1}`,
			false,
		},
		{
			"Method Y, Vote wrong option",
			dsmodels.Poll{
				Pollmethod: "Y",
				OptionIDs:  []int{1, 2},
			},
			`{"5":1}`,
			false,
		},
		{
			"Method Y and maxVotesPerOption>1, Correct vote",
			dsmodels.Poll{
				Pollmethod:        "Y",
				OptionIDs:         []int{1, 2, 3, 4},
				MaxVotesAmount:    6,
				MaxVotesPerOption: 3,
			},
			`{"1":2,"2":0,"3":3,"4":1}`,
			true,
		},
		{
			"Method Y and maxVotesPerOption>1, Too many votes on one option",
			dsmodels.Poll{
				Pollmethod:        "Y",
				OptionIDs:         []int{1, 2},
				MaxVotesAmount:    4,
				MaxVotesPerOption: 2,
			},
			`{"1":3,"2":1}`,
			false,
		},
		{
			"Method Y and maxVotesPerOption>1, Too many votes in total",
			dsmodels.Poll{
				Pollmethod:        "Y",
				OptionIDs:         []int{1, 2},
				MaxVotesAmount:    3,
				MaxVotesPerOption: 2,
			},
			`{"1":2,"2":2}`,
			false,
		},

		// Test Method YN and YNA
		{
			"Method YN, Global Y, Vote Y",
			dsmodels.Poll{
				Pollmethod: "YN",
				GlobalYes:  true,
			},
			`"Y"`,
			true,
		},
		{
			"Method YN, Not Global Y, Vote Y",
			dsmodels.Poll{
				Pollmethod: "YN",
				GlobalYes:  false,
			},
			`"Y"`,
			false,
		},
		{
			"Method YNA, Global N, Vote N",
			dsmodels.Poll{
				Pollmethod: "YNA",
				GlobalNo:   true,
			},
			`"N"`,
			true,
		},
		{
			"Method YNA, Not Global N, Vote N",
			dsmodels.Poll{
				Pollmethod: "YNA",
				GlobalYes:  false,
			},
			`"N"`,
			false,
		},
		{
			"Method YNA, Y on Option",
			dsmodels.Poll{
				Pollmethod: "YNA",
				OptionIDs:  []int{1, 2},
			},
			`{"1":"Y"}`,
			true,
		},
		{
			"Method YNA, N on Option",
			dsmodels.Poll{
				Pollmethod: "YNA",
				OptionIDs:  []int{1, 2},
			},
			`{"1":"N"}`,
			true,
		},
		{
			"Method YNA, A on Option",
			dsmodels.Poll{
				Pollmethod: "YNA",
				OptionIDs:  []int{1, 2},
			},
			`{"1":"A"}`,
			true,
		},
		{
			"Method YN, A on Option",
			dsmodels.Poll{
				Pollmethod: "YN",
				OptionIDs:  []int{1, 2},
			},
			`{"1":"A"}`,
			false,
		},
		{
			"Method YN, Y on wrong Option",
			dsmodels.Poll{
				Pollmethod: "YN",
				OptionIDs:  []int{1, 2},
			},
			`{"3":"Y"}`,
			false,
		},
		{
			"Method YNA, Vote on many Options",
			dsmodels.Poll{
				Pollmethod: "YNA",
				OptionIDs:  []int{1, 2, 3},
			},
			`{"1":"Y","2":"N","3":"A"}`,
			true,
		},
		{
			"Method YNA, Amount on Option",
			dsmodels.Poll{
				Pollmethod: "YNA",
				OptionIDs:  []int{1, 2, 3},
			},
			`{"1":1}`,
			false,
		},
		{
			"Method YNA with to low selected",
			dsmodels.Poll{
				Pollmethod:     "YNA",
				OptionIDs:      []int{1, 2, 3},
				MinVotesAmount: 2,
			},
			`{"1":"Y"}`,
			false,
		},
		{
			"Method YNA with enough selected",
			dsmodels.Poll{
				Pollmethod:     "YNA",
				OptionIDs:      []int{1, 2, 3},
				MinVotesAmount: 2,
			},
			`{"1":"Y", "2":"N"}`,
			true,
		},
		{
			"Method YNA with to many selected",
			dsmodels.Poll{
				Pollmethod:     "YNA",
				OptionIDs:      []int{1, 2, 3},
				MaxVotesAmount: 2,
			},
			`{"1":"Y", "2":"N", "3":"A"}`,
			false,
		},
		{
			"Method YNA with not to many selected",
			dsmodels.Poll{
				Pollmethod:     "YNA",
				OptionIDs:      []int{1, 2, 3},
				MaxVotesAmount: 2,
			},
			`{"1":"Y", "2":"N"}`,
			true,
		},

		// Unknown method
		{
			"Method Unknown",
			dsmodels.Poll{
				Pollmethod: "XXX",
			},
			`"Y"`,
			false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var b ballot
			if err := json.Unmarshal([]byte(tt.vote), &b.Value); err != nil {
				t.Fatalf("decoding vote: %v", err)
			}

			validation := validate(tt.poll, b.Value)

			if tt.expectValid {
				if validation != "" {
					t.Fatalf("Validate returned unexpected message: %v", validation)
				}
				return
			}

			if validation == "" {
				t.Fatalf("Got no validation error")
			}
		})
	}
}
