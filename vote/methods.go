package vote

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/OpenSlides/openslides-go/fastjson"
	"github.com/shopspring/decimal"
)

type method interface {
	Name() string
	Validate(config json.RawMessage, vote json.RawMessage) error
	Result(config json.RawMessage, votes []dsmodels.Vote) ([]byte, error)
}

type methodMotion struct{}

func (m methodMotion) Name() string {
	return "motion"
}

func (m methodMotion) Validate(config json.RawMessage, vote json.RawMessage) error {
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

func (m methodMotion) Result(config json.RawMessage, votes []dsmodels.Vote) ([]byte, error) {
	var result struct {
		Yes     decimal.Decimal `json:"yes"`
		No      decimal.Decimal `json:"no"`
		Abstain decimal.Decimal `json:"abstain,omitzero"`
		Invalid decimal.Decimal `json:"invalid,omitzero"`
		Base    int             `json:"base"`
	}

	for _, vote := range votes {
		if err := m.Validate(config, json.RawMessage(vote.Value)); err != nil {
			result.Invalid = result.Invalid.Add(decimal.NewFromInt(1))
			continue
		}

		weight := vote.Weight
		if weight == "" {
			weight = "1"
		}
		factor, err := decimal.NewFromString(weight)
		if err != nil {
			return nil, fmt.Errorf("invalid weight `%s` in vote %d: %w", vote.Weight, vote.ID, err)
		}

		switch strings.ToLower(string(vote.Value)) {
		case `"yes"`:
			result.Yes = result.Yes.Add(factor)
		case `"no"`:
			result.No = result.No.Add(factor)
		case `"abstain"`:
			result.Abstain = result.Abstain.Add(factor)
		}

	}
	// TODO: Calc base
	result.Base = len(votes) - int(result.Invalid.IntPart())

	encodedResult, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode result: %w", err)
	}

	return encodedResult, nil
}

type methodSelection struct{}

func (m methodSelection) Name() string {
	return "selection"
}

func (m methodSelection) Validate(config json.RawMessage, vote json.RawMessage) error {
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

func (m methodSelection) Result(config json.RawMessage, votes []dsmodels.Vote) ([]byte, error) {
	return nil, nil
}

type methodRating struct{}

func (m methodRating) Name() string {
	return "rating"
}

func (m methodRating) Validate(config json.RawMessage, vote json.RawMessage) error {
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

func (m methodRating) Result(config json.RawMessage, votes []dsmodels.Vote) ([]byte, error) {
	return nil, nil
}
