package vote

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/fastjson"
)

func ValidateVote(method string, config json.RawMessage, vote json.RawMessage) error {
	switch method {
	case "motion":
		return validateMotion(config, vote)
	case "selection":
		return validateSelection(config, vote)
	case "rating":
		return validateRating(config, vote)
	default:
		return fmt.Errorf("unknown poll method: %s", method)
	}
}

func validateMotion(config json.RawMessage, vote json.RawMessage) error {
	var cfg struct {
		Abstain dsfetch.Maybe[bool] `json:"abstain"`
	}

	if config != nil {
		if err := json.Unmarshal(config, &cfg); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
	}

	switch strings.ToLower(string(vote)) {
	case `"yes"`, `"no"`:
		return nil
	case `"abstain"`:
		if abstain, set := cfg.Abstain.Value(); !abstain && set {
			return fmt.Errorf("abstain disabled")
		}
		return nil
	default:
		return fmt.Errorf("Unknown value %s", vote)
	}
}

func validateSelection(config json.RawMessage, vote json.RawMessage) error {
	var cfg struct {
		Options          []string           `json:"options"`
		MaxOptionsAmount dsfetch.Maybe[int] `json:"max_options_amount"`
		MinOptionsAmount dsfetch.Maybe[int] `json:"min_options_amount"`
	}

	if err := json.Unmarshal(config, &cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	choice, err := fastjson.DecodeIntList(vote)
	if err != nil {
		return fmt.Errorf("decoding vote: %w", err)
	}

	if value, set := cfg.MaxOptionsAmount.Value(); set && len(choice) > value {
		return fmt.Errorf("too many options")
	}

	if value, set := cfg.MinOptionsAmount.Value(); set && len(choice) < value {
		return fmt.Errorf("too few options")
	}
	for _, option := range choice {
		if option < 0 || option >= len(cfg.Options) {
			return fmt.Errorf("invalid option %d", option)
		}
	}

	return nil
}

func validateRating(config json.RawMessage, vote json.RawMessage) error {
	var cfg struct {
		Options           []string           `json:"options"`
		MaxOptionsAmount  dsfetch.Maybe[int] `json:"max_options_amount"`
		MinOptionsAmount  dsfetch.Maybe[int] `json:"min_options_amount"`
		MaxVotesPerOption dsfetch.Maybe[int] `json:"max_votes_per_option"`
		MaxVoteSum        dsfetch.Maybe[int] `json:"max_vote_sum"`
		MinVoteSum        dsfetch.Maybe[int] `json:"min_vote_sum"`
	}

	if err := json.Unmarshal(config, &cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	var choice map[int]int
	if err := json.Unmarshal(vote, &choice); err != nil {
		return fmt.Errorf("decoding vote: %w", err)
	}

	if value, set := cfg.MaxOptionsAmount.Value(); set && len(choice) > value {
		return fmt.Errorf("too many options")
	}

	if value, set := cfg.MinOptionsAmount.Value(); set && len(choice) < value {
		return fmt.Errorf("too few options")
	}

	var sum int
	for option, choice := range choice {
		if option < 0 || option >= len(cfg.Options) {
			return fmt.Errorf("invalid option %d", option)
		}

		if choice < 0 {
			return fmt.Errorf("negative value for option")
		}

		if value, set := cfg.MaxVotesPerOption.Value(); set {
			if choice > value {
				return fmt.Errorf("too many votes for option")
			}
		}
		sum += choice
	}

	if value, set := cfg.MaxVoteSum.Value(); set && sum > value {
		return fmt.Errorf("too many votes")
	}

	if value, set := cfg.MinVoteSum.Value(); set && sum < value {
		return fmt.Errorf("too few votes")
	}

	return nil
}
