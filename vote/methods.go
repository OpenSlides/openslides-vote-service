package vote

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/shopspring/decimal"
)

type method interface {
	Name() string
	ValidateConfig(config string) error
	ValidateVote(config string, vote json.RawMessage) error
	Result(config string, votes []dsmodels.Vote) (string, error)
}

type methodMotion struct{}

func (m methodMotion) Name() string {
	return "motion"
}

func (m methodMotion) ValidateConfig(config string) error {
	var cfg struct {
		Abstain dsfetch.Maybe[bool] `json:"abstain"`
	}

	if config != "" {
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			return MessageErrorf(ErrInvalid, "Invalid json: %v", err)
		}
	}

	return nil
}

func (m methodMotion) ValidateVote(config string, vote json.RawMessage) error {
	var cfg struct {
		Abstain dsfetch.Maybe[bool] `json:"abstain"`
	}

	if config != "" {
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
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

func (m methodMotion) Result(config string, votes []dsmodels.Vote) (string, error) {
	return iterateValues(m, config, votes, func(value string, weight decimal.Decimal, result map[string]decimal.Decimal) error {
		switch strings.ToLower(value) {
		case `"yes"`:
			result["yes"] = result["yes"].Add(weight)
		case `"no"`:
			result["no"] = result["no"].Add(weight)
		case `"abstain"`:
			result["abstain"] = result["abstain"].Add(weight)
		}
		return nil
	})
}

type methodSelection struct{}

func (m methodSelection) Name() string {
	return "selection"
}

func (m methodSelection) ValidateConfig(config string) error {
	var cfg struct {
		Options          []json.RawMessage  `json:"options"`
		MaxOptionsAmount dsfetch.Maybe[int] `json:"max_options_amount"`
		MinOptionsAmount dsfetch.Maybe[int] `json:"min_options_amount"`
	}

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return MessageErrorf(ErrInvalid, "Invalid json: %v", err)
	}

	if len(cfg.Options) == 0 {
		return MessageError(ErrInvalid, "Poll with method selection needs at least one option")
	}

	return nil
}

func (m methodSelection) ValidateVote(config string, vote json.RawMessage) error {
	var cfg struct {
		Options          []json.RawMessage  `json:"options"`
		MaxOptionsAmount dsfetch.Maybe[int] `json:"max_options_amount"`
		MinOptionsAmount dsfetch.Maybe[int] `json:"min_options_amount"`
	}

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
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

func (m methodSelection) Result(config string, votes []dsmodels.Vote) (string, error) {
	return iterateValues(m, config, votes, func(value string, weight decimal.Decimal, result map[string]decimal.Decimal) error {
		var votedOptions []int
		if err := json.Unmarshal([]byte(value), &votedOptions); err != nil {
			return fmt.Errorf("invalid options `%s`: %w", value, err)
		}

		for _, votedOption := range votedOptions {
			result[strconv.Itoa(votedOption)] = result[strconv.Itoa(votedOption)].Add(weight)
		}

		if len(votedOptions) == 0 {
			result["abstain"] = result["abstain"].Add(weight)
		}

		return nil
	})
}

type methodRating struct{}

func (m methodRating) Name() string {
	return "rating"
}

func (m methodRating) ValidateConfig(config string) error {
	var cfg struct {
		Options           []json.RawMessage  `json:"options"`
		MaxOptionsAmount  dsfetch.Maybe[int] `json:"max_options_amount"`
		MinOptionsAmount  dsfetch.Maybe[int] `json:"min_options_amount"`
		MaxVotesPerOption dsfetch.Maybe[int] `json:"max_votes_per_option"`
		MaxVoteSum        dsfetch.Maybe[int] `json:"max_vote_sum"`
		MinVoteSum        dsfetch.Maybe[int] `json:"min_vote_sum"`
	}

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return MessageErrorf(ErrInvalid, "Invalid json: %v", err)
	}

	if len(cfg.Options) == 0 {
		return MessageError(ErrInvalid, "Poll with method rating needs at least one option")
	}

	return nil
}

func (m methodRating) ValidateVote(config string, vote json.RawMessage) error {
	var cfg struct {
		Options           []json.RawMessage  `json:"options"`
		MaxOptionsAmount  dsfetch.Maybe[int] `json:"max_options_amount"`
		MinOptionsAmount  dsfetch.Maybe[int] `json:"min_options_amount"`
		MaxVotesPerOption dsfetch.Maybe[int] `json:"max_votes_per_option"`
		MaxVoteSum        dsfetch.Maybe[int] `json:"max_vote_sum"`
		MinVoteSum        dsfetch.Maybe[int] `json:"min_vote_sum"`
	}

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
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

func (m methodRating) Result(config string, votes []dsmodels.Vote) (string, error) {
	return iterateValues(m, config, votes, func(value string, weight decimal.Decimal, result map[string]decimal.Decimal) error {
		var votedOptions map[int]int
		if err := json.Unmarshal([]byte(value), &votedOptions); err != nil {
			return fmt.Errorf("invalid options `%s`: %w", value, err)
		}

		for votedOption, value := range votedOptions {
			voteWithFactor := weight.Mul(decimal.NewFromInt(int64(value)))
			result[strconv.Itoa(votedOption)] = result[strconv.Itoa(votedOption)].Add(voteWithFactor)
		}

		if len(votedOptions) == 0 {
			result["abstain"] = result["abstain"].Add(weight)
		}

		return nil
	})
}

type methodRatingMotion struct{}

func (m methodRatingMotion) Name() string {
	return "rating-motion"
}

func (m methodRatingMotion) ValidateConfig(config string) error {
	var cfg struct {
		Options          []json.RawMessage   `json:"options"`
		MaxOptionsAmount dsfetch.Maybe[int]  `json:"max_options_amount"`
		MinOptionsAmount dsfetch.Maybe[int]  `json:"min_options_amount"`
		Abstain          dsfetch.Maybe[bool] `json:"abstain"`
	}

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return MessageErrorf(ErrInvalid, "Invalid json: %v", err)
	}

	if len(cfg.Options) == 0 {
		return MessageError(ErrInvalid, "Poll with method rating-motion needs at least one option")
	}

	return nil
}

func (m methodRatingMotion) ValidateVote(config string, vote json.RawMessage) error {
	var cfg struct {
		Options          []json.RawMessage   `json:"options"`
		MaxOptionsAmount dsfetch.Maybe[int]  `json:"max_options_amount"`
		MinOptionsAmount dsfetch.Maybe[int]  `json:"min_options_amount"`
		Abstain          dsfetch.Maybe[bool] `json:"abstain"`
	}

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	var choice map[int]json.RawMessage
	if err := json.Unmarshal(vote, &choice); err != nil {
		return errors.Join(invalidVote("Vote has invalid format"), fmt.Errorf("decoding vote: %w", err))
	}

	if value, set := cfg.MaxOptionsAmount.Value(); set && len(choice) > value {
		return invalidVote("too many options")
	}

	if value, set := cfg.MinOptionsAmount.Value(); set && len(choice) < value {
		return invalidVote("too few options")
	}

	for option, choice := range choice {
		if err := (methodMotion{}).ValidateVote(config, choice); err != nil {
			return fmt.Errorf("validating option %d: %w", option, err)
		}
	}

	return nil
}

type DecimalOrMap struct {
	decimal decimal.Decimal
	values  map[string]decimal.Decimal
}

func (m methodRatingMotion) Result(config string, votes []dsmodels.Vote) (string, error) {
	result := make(map[int]map[string]decimal.Decimal)
	invalid := 0

	for _, vote := range votes {
		if err := m.ValidateVote(config, json.RawMessage(vote.Value)); err != nil {
			if errors.Is(err, ErrInvalid) {
				invalid += 1
				continue
			}
			return "", fmt.Errorf("validating vote: %w", err)
		}

		weightStr := vote.Weight
		if weightStr == "" {
			weightStr = "1"
		}
		weight, err := decimal.NewFromString(weightStr)
		if err != nil {
			return "", fmt.Errorf("invalid weight `%s` in vote %d: %w", vote.Weight, vote.ID, err)
		}

		var votedOptions map[int]json.RawMessage
		if err := json.Unmarshal([]byte(vote.Value), &votedOptions); err != nil {
			return "", fmt.Errorf("invalid options `%s`: %w", vote.Value, err)
		}

		for optionIdx, value := range votedOptions {
			if _, ok := result[optionIdx]; !ok {
				result[optionIdx] = make(map[string]decimal.Decimal)
			}

			switch strings.ToLower(string(value)) {
			case `"yes"`:
				result[optionIdx]["yes"] = result[optionIdx]["yes"].Add(weight)
			case `"no"`:
				result[optionIdx]["no"] = result[optionIdx]["no"].Add(weight)
			case `"abstain"`:
				result[optionIdx]["abstain"] = result[optionIdx]["abstain"].Add(weight)
			}
		}
	}

	encodedResult, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode result: %w", err)
	}
	withInvalid, err := addInvalid(encodedResult, invalid)
	if err != nil {
		return "", fmt.Errorf("add invalid: %w", err)
	}
	return string(withInvalid), nil
}

func addInvalid(result []byte, invalid int) ([]byte, error) {
	if invalid == 0 {
		return result, nil
	}

	var data map[string]any
	if err := json.Unmarshal(result, &data); err != nil {
		return nil, err
	}

	data["invalid"] = invalid

	return json.Marshal(data)
}

func iterateValues(
	m method,
	config string,
	votes []dsmodels.Vote,
	fn func(value string, weight decimal.Decimal, result map[string]decimal.Decimal) error,
) (string, error) {
	result := make(map[string]decimal.Decimal)
	invalid := 0
	for _, vote := range votes {
		if err := m.ValidateVote(config, json.RawMessage(vote.Value)); err != nil {
			if errors.Is(err, ErrInvalid) {
				invalid += 1
				continue
			}
			return "", fmt.Errorf("validating vote: %w", err)
		}

		weight := vote.Weight
		if weight == "" {
			weight = "1"
		}
		factor, err := decimal.NewFromString(weight)
		if err != nil {
			return "", fmt.Errorf("invalid weight `%s` in vote %d: %w", vote.Weight, vote.ID, err)
		}

		if err := fn(vote.Value, factor, result); err != nil {
			return "", fmt.Errorf("prcess: %w", err)
		}
	}

	encodedResult, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode result: %w", err)
	}

	withInvalid, err := addInvalid(encodedResult, invalid)
	if err != nil {
		return "", fmt.Errorf("add invalid: %w", err)
	}
	return string(withInvalid), nil
}
