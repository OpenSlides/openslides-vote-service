package vote

import (
	"encoding/json"
	"testing"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
)

func TestVoteValidate(t *testing.T) {
	for _, tt := range []struct {
		name        string
		poll        dsfetch.Poll
		vote        string
		expectValid bool
	}{
		// Test Method Y and N.
		{
			"Method Y, Global Y, Vote Y",
			dsfetch.Poll{
				Pollmethod: "Y",
				GlobalYes:  true,
			},
			`"Y"`,
			true,
		},
		{
			"Method Y, Vote Y",
			dsfetch.Poll{
				Pollmethod: "Y",
				GlobalYes:  false,
			},
			`"Y"`,
			false,
		},
		{
			"Method Y, Vote N",
			dsfetch.Poll{
				Pollmethod: "Y",
				GlobalNo:   false,
			},
			`"N"`,
			false,
		},
		{
			// The poll config is invalid. A poll with method Y should not allow global_no.
			"Method Y, Global N, Vote N",
			dsfetch.Poll{
				Pollmethod: "Y",
				GlobalNo:   true,
			},
			`"N"`,
			true,
		},
		{
			"Method N, Global N, Vote N",
			dsfetch.Poll{
				Pollmethod: "N",
				GlobalNo:   true,
			},
			`"N"`,
			true,
		},
		{
			"Method Y, Vote Option",
			dsfetch.Poll{
				Pollmethod: "Y",
				OptionIDs:  []int{1, 2},
			},
			`{"1":1}`,
			true,
		},
		{
			"Method Y, Vote on to many Options",
			dsfetch.Poll{
				Pollmethod: "Y",
				OptionIDs:  []int{1, 2},
			},
			`{"1":1,"2":1}`,
			false,
		},
		{
			"Method Y, Vote on one option with to high amount",
			dsfetch.Poll{
				Pollmethod: "Y",
				OptionIDs:  []int{1, 2},
			},
			`{"1":5}`,
			false,
		},
		{
			"Method Y, Vote on many option with to high amount",
			dsfetch.Poll{
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
			dsfetch.Poll{
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
			dsfetch.Poll{
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
			dsfetch.Poll{
				Pollmethod:     "Y",
				OptionIDs:      []int{1, 2},
				MinVotesAmount: 10,
			},
			`{"1":1,"2":1}`,
			false,
		},
		{
			"Method Y, Vote on one option with -1 amount",
			dsfetch.Poll{
				Pollmethod: "Y",
				OptionIDs:  []int{1, 2},
			},
			`{"1":-1}`,
			false,
		},
		{
			"Method Y, Vote wrong option",
			dsfetch.Poll{
				Pollmethod: "Y",
				OptionIDs:  []int{1, 2},
			},
			`{"5":1}`,
			false,
		},
		{
			"Method Y and maxVotesPerOption>1, Correct vote",
			dsfetch.Poll{
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
			dsfetch.Poll{
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
			dsfetch.Poll{
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
			dsfetch.Poll{
				Pollmethod: "YN",
				GlobalYes:  true,
			},
			`"Y"`,
			true,
		},
		{
			"Method YN, Not Global Y, Vote Y",
			dsfetch.Poll{
				Pollmethod: "YN",
				GlobalYes:  false,
			},
			`"Y"`,
			false,
		},
		{
			"Method YNA, Global N, Vote N",
			dsfetch.Poll{
				Pollmethod: "YNA",
				GlobalNo:   true,
			},
			`"N"`,
			true,
		},
		{
			"Method YNA, Not Global N, Vote N",
			dsfetch.Poll{
				Pollmethod: "YNA",
				GlobalYes:  false,
			},
			`"N"`,
			false,
		},
		{
			"Method YNA, Y on Option",
			dsfetch.Poll{
				Pollmethod: "YNA",
				OptionIDs:  []int{1, 2},
			},
			`{"1":"Y"}`,
			true,
		},
		{
			"Method YNA, N on Option",
			dsfetch.Poll{
				Pollmethod: "YNA",
				OptionIDs:  []int{1, 2},
			},
			`{"1":"N"}`,
			true,
		},
		{
			"Method YNA, A on Option",
			dsfetch.Poll{
				Pollmethod: "YNA",
				OptionIDs:  []int{1, 2},
			},
			`{"1":"A"}`,
			true,
		},
		{
			"Method YN, A on Option",
			dsfetch.Poll{
				Pollmethod: "YN",
				OptionIDs:  []int{1, 2},
			},
			`{"1":"A"}`,
			false,
		},
		{
			"Method YN, Y on wrong Option",
			dsfetch.Poll{
				Pollmethod: "YN",
				OptionIDs:  []int{1, 2},
			},
			`{"3":"Y"}`,
			false,
		},
		{
			"Method YNA, Vote on many Options",
			dsfetch.Poll{
				Pollmethod: "YNA",
				OptionIDs:  []int{1, 2, 3},
			},
			`{"1":"Y","2":"N","3":"A"}`,
			true,
		},
		{
			"Method YNA, Amount on Option",
			dsfetch.Poll{
				Pollmethod: "YNA",
				OptionIDs:  []int{1, 2, 3},
			},
			`{"1":1}`,
			false,
		},

		// Unknown method
		{
			"Method Unknown",
			dsfetch.Poll{
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
