package vote

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
			return invalidVote("abstain disabled")
		}
		return nil
	default:
		return invalidVote("Unknown value %s", vote)
	}
}

func (m methodMotion) Result(config json.RawMessage, votes []dsmodels.Vote) ([]byte, error) {
	var result struct {
		Yes     decimal.Decimal `json:"yes"`
		No      decimal.Decimal `json:"no"`
		Abstain decimal.Decimal `json:"abstain,omitzero"`
		Invalid decimal.Decimal `json:"invalid,omitzero"`
	}

	for _, vote := range votes {
		if err := m.Validate(config, json.RawMessage(vote.Value)); err != nil {
			if errors.Is(err, ErrInvalid) {
				result.Invalid = result.Invalid.Add(decimal.NewFromInt(1))
				continue
			}
			return nil, fmt.Errorf("validating vote: %w", err)
		}

		weight := vote.Weight
		if weight == "" {
			weight = "1"
		}
		factor, err := decimal.NewFromString(weight)
		if err != nil {
			return nil, fmt.Errorf("invalid weight `%s` in vote %d: %w", vote.Weight, vote.ID, err)
		}

		switch strings.ToLower(vote.Value) {
		case `"yes"`:
			result.Yes = result.Yes.Add(factor)
		case `"no"`:
			result.No = result.No.Add(factor)
		case `"abstain"`:
			result.Abstain = result.Abstain.Add(factor)
		}
	}

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
		Options          []json.RawMessage  `json:"options"`
		MaxOptionsAmount dsfetch.Maybe[int] `json:"max_options_amount"`
		MinOptionsAmount dsfetch.Maybe[int] `json:"min_options_amount"`
	}

	if err := json.Unmarshal(config, &cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	var choice []int
	if err := json.Unmarshal(vote, &choice); err != nil {
		return errors.Join(invalidVote("Vote has invalid format"), fmt.Errorf("decoding vote: %w", err))
	}

	if value, set := cfg.MaxOptionsAmount.Value(); set && len(choice) > value {
		return invalidVote("too many options")
	}

	if value, set := cfg.MinOptionsAmount.Value(); set && len(choice) < value {
		return invalidVote("too few options")
	}
	for _, option := range choice {
		if option < 0 || option >= len(cfg.Options) {
			return invalidVote("invalid option %d", option)
		}
	}

	return nil
}

func (m methodSelection) Result(config json.RawMessage, votes []dsmodels.Vote) ([]byte, error) {
	result := make(map[string]decimal.Decimal)

	for _, vote := range votes {
		if err := m.Validate(config, json.RawMessage(vote.Value)); err != nil {
			if errors.Is(err, ErrInvalid) {
				result["invalid"] = result["invalid"].Add(decimal.NewFromInt(1))
				continue
			}
			return nil, fmt.Errorf("validating vote: %w", err)
		}

		weight := vote.Weight
		if weight == "" {
			weight = "1"
		}
		factor, err := decimal.NewFromString(weight)
		if err != nil {
			return nil, fmt.Errorf("invalid weight `%s` in vote %d: %w", vote.Weight, vote.ID, err)
		}

		votedOptions, err := fastjson.DecodeIntList([]byte(vote.Value))
		if err != nil {
			return nil, fmt.Errorf("invalid options `%s` in vote %d: %w", vote.Value, vote.ID, err)
		}

		for _, votedOption := range votedOptions {
			result[strconv.Itoa(votedOption)] = result[strconv.Itoa(votedOption)].Add(factor)
		}
	}

	encodedResult, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode result: %w", err)
	}

	return encodedResult, nil
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
		return errors.Join(invalidVote("Vote has invalid format"), fmt.Errorf("decoding vote: %w", err))
	}

	if value, set := cfg.MaxOptionsAmount.Value(); set && len(choice) > value {
		return invalidVote("too many options")
	}

	if value, set := cfg.MinOptionsAmount.Value(); set && len(choice) < value {
		return invalidVote("too few options")
	}

	var sum int
	for option, choice := range choice {
		if option < 0 || option >= len(cfg.Options) {
			return invalidVote("invalid option %d", option)
		}

		if choice < 0 {
			return invalidVote("negative value for option")
		}

		if value, set := cfg.MaxVotesPerOption.Value(); set {
			if choice > value {
				return invalidVote("too many votes for option")
			}
		}
		sum += choice
	}

	if value, set := cfg.MaxVoteSum.Value(); set && sum > value {
		return invalidVote("too many votes")
	}

	if value, set := cfg.MinVoteSum.Value(); set && sum < value {
		return invalidVote("too few votes")
	}

	return nil
}

func (m methodRating) Result(config json.RawMessage, votes []dsmodels.Vote) ([]byte, error) {
	return nil, nil
}
