package vote

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/shopspring/decimal"
)

const (
	keyAbstain = "abstain"
	keyNota    = "nota"
	keyInvalid = "invalid"
)

var reservedOptionNames = []string{keyAbstain, keyNota, keyInvalid}

type method interface {
	Name() string
	ValidateConfig(config string) error
	ValidateVote(config string, vote json.RawMessage) error
	Result(config string, votes []dsmodels.Vote) (string, error)
}

type methodApprovalConfig struct {
	AllowAbstain dsfetch.Maybe[bool] `json:"allow_abstain"`
}

type methodApproval struct{}

func (m methodApproval) Name() string {
	return "approval"
}

func (m methodApproval) ValidateConfig(config string) error {
	var cfg methodApprovalConfig

	if config != "" {
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			return MessageErrorf(ErrInvalid, "Invalid json: %v", err)
		}
	}

	return nil
}

func (m methodApproval) ValidateVote(config string, vote json.RawMessage) error {
	var cfg methodApprovalConfig

	if config != "" {
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
	}

	switch strings.ToLower(string(vote)) {
	case `"yes"`, `"no"`:
		return nil
	case `"abstain"`:
		if abstain, set := cfg.AllowAbstain.Value(); !abstain && set {
			return invalidVote("abstain disabled")
		}
		return nil
	default:
		return invalidVote("Unknown value %s", vote)
	}
}

func (m methodApproval) Result(config string, votes []dsmodels.Vote) (string, error) {
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

type methodSelectionConfig struct {
	Options          map[string]json.RawMessage `json:"options"`
	MaxOptionsAmount dsfetch.Maybe[int]         `json:"max_options_amount"`
	MinOptionsAmount dsfetch.Maybe[int]         `json:"min_options_amount"`
	AllowNota        bool                       `json:"allow_nota"`
}

type methodSelection struct{}

func (m methodSelection) Name() string {
	return "selection"
}

func (m methodSelection) ValidateConfig(config string) error {
	var cfg methodSelectionConfig

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return MessageErrorf(ErrInvalid, "Invalid json: %v", err)
	}

	if len(cfg.Options) == 0 {
		return MessageError(ErrInvalid, "Poll with method selection needs at least one option")
	}

	for key := range cfg.Options {
		if slices.Contains(reservedOptionNames, key) {
			return MessageErrorf(ErrInternal, "%s is not allowed as an option key", key)
		}
	}

	return nil
}

func (m methodSelection) ValidateVote(config string, vote json.RawMessage) error {
	var cfg methodSelectionConfig

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	var choice []string
	if err := json.Unmarshal(vote, &choice); err != nil {
		if cfg.AllowNota && strings.ToLower(string(vote)) == `"nota"` {
			return nil
		}
		return errors.Join(invalidVote("Vote has invalid format"), fmt.Errorf("decoding vote: %w", err))
	}

	if hasDuplicates(choice) {
		return invalidVote("douplicate entries in vote")
	}

	if value, set := cfg.MaxOptionsAmount.Value(); set && len(choice) > value {
		return invalidVote("too many options")
	}

	if value, set := cfg.MinOptionsAmount.Value(); set && len(choice) < value {
		return invalidVote("too few options")
	}
	for _, option := range choice {
		if _, ok := cfg.Options[option]; !ok {
			return invalidVote("unknown option %s", option)
		}
	}

	return nil
}

func (m methodSelection) Result(config string, votes []dsmodels.Vote) (string, error) {
	var cfg methodSelectionConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return "", fmt.Errorf("invalid configuration: %w", err)
	}

	return iterateValues(m, config, votes, func(value string, weight decimal.Decimal, result map[string]decimal.Decimal) error {
		var votedOptions []string
		if err := json.Unmarshal([]byte(value), &votedOptions); err != nil {
			if cfg.AllowNota && strings.ToLower(value) == `"nota"` {
				result[keyNota] = result[keyNota].Add(weight)
				return nil
			}
			return fmt.Errorf("invalid options `%s`: %w", value, err)
		}

		for _, votedOption := range votedOptions {
			result[votedOption] = result[votedOption].Add(weight)
		}

		if len(votedOptions) == 0 {
			result[keyAbstain] = result[keyAbstain].Add(weight)
		}

		return nil
	})
}

type methodRatingScoreConfig struct {
	Options           map[string]json.RawMessage `json:"options"`
	MaxOptionsAmount  dsfetch.Maybe[int]         `json:"max_options_amount"`
	MinOptionsAmount  dsfetch.Maybe[int]         `json:"min_options_amount"`
	MaxVotesPerOption dsfetch.Maybe[int]         `json:"max_votes_per_option"`
	MaxVoteSum        dsfetch.Maybe[int]         `json:"max_vote_sum"`
	MinVoteSum        dsfetch.Maybe[int]         `json:"min_vote_sum"`
}

type methodRatingScore struct{}

func (m methodRatingScore) Name() string {
	return "rating-score"
}

func (m methodRatingScore) ValidateConfig(config string) error {
	var cfg methodRatingScoreConfig

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return MessageErrorf(ErrInvalid, "Invalid json: %v", err)
	}

	if len(cfg.Options) == 0 {
		return MessageError(ErrInvalid, "Poll with method rating-score needs at least one option")
	}

	for key := range cfg.Options {
		if slices.Contains(reservedOptionNames, key) {
			return MessageErrorf(ErrInternal, "%s is not allowed as an option key", key)
		}
	}

	return nil
}

func (m methodRatingScore) ValidateVote(config string, vote json.RawMessage) error {
	var cfg methodRatingScoreConfig

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	var choice map[string]int
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
		if _, ok := cfg.Options[option]; !ok {
			return invalidVote("unknown option %s", option)
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

func (m methodRatingScore) Result(config string, votes []dsmodels.Vote) (string, error) {
	return iterateValues(m, config, votes, func(value string, weight decimal.Decimal, result map[string]decimal.Decimal) error {
		var votedOptions map[string]int
		if err := json.Unmarshal([]byte(value), &votedOptions); err != nil {
			return fmt.Errorf("invalid options `%s`: %w", value, err)
		}

		for votedOption, value := range votedOptions {
			voteWithFactor := weight.Mul(decimal.NewFromInt(int64(value)))
			result[votedOption] = result[votedOption].Add(voteWithFactor)
		}

		if len(votedOptions) == 0 {
			result[keyAbstain] = result[keyAbstain].Add(weight)
		}

		return nil
	})
}

type methodRatingApprovalConfig struct {
	Options          map[string]json.RawMessage `json:"options"`
	MaxOptionsAmount dsfetch.Maybe[int]         `json:"max_options_amount"`
	MinOptionsAmount dsfetch.Maybe[int]         `json:"min_options_amount"`
	AllowAbstain     dsfetch.Maybe[bool]        `json:"allow_abstain"`
}

type methodRatingApproval struct{}

func (m methodRatingApproval) Name() string {
	return "rating-approval"
}

func (m methodRatingApproval) ValidateConfig(config string) error {
	var cfg methodRatingApprovalConfig

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return MessageErrorf(ErrInvalid, "Invalid json: %v", err)
	}

	if len(cfg.Options) == 0 {
		return MessageError(ErrInvalid, "Poll with method rating-approval needs at least one option")
	}

	for key := range cfg.Options {
		if slices.Contains(reservedOptionNames, key) {
			return MessageErrorf(ErrInternal, "%s is not allowed as an option key", key)
		}
	}

	return nil
}

func (m methodRatingApproval) ValidateVote(config string, vote json.RawMessage) error {
	var cfg methodRatingApprovalConfig

	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	var choice map[string]json.RawMessage
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
		if _, ok := cfg.Options[option]; !ok {
			return invalidVote("unknown option %s", option)
		}

		if err := (methodApproval{}).ValidateVote(config, choice); err != nil {
			return fmt.Errorf("validating option %s: %w", option, err)
		}
	}

	return nil
}

type DecimalOrMap struct {
	decimal decimal.Decimal
	values  map[string]decimal.Decimal
}

func (m methodRatingApproval) Result(config string, votes []dsmodels.Vote) (string, error) {
	result := make(map[string]map[string]decimal.Decimal)
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

		var votedOptions map[string]json.RawMessage
		if err := json.Unmarshal([]byte(vote.Value), &votedOptions); err != nil {
			return "", fmt.Errorf("invalid options `%s`: %w", vote.Value, err)
		}

		for option, value := range votedOptions {
			if _, ok := result[option]; !ok {
				result[option] = make(map[string]decimal.Decimal)
			}

			switch strings.ToLower(string(value)) {
			case `"yes"`:
				result[option]["yes"] = result[option]["yes"].Add(weight)
			case `"no"`:
				result[option]["no"] = result[option]["no"].Add(weight)
			case `"abstain"`:
				result[option]["abstain"] = result[option]["abstain"].Add(weight)
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

	data[keyInvalid] = invalid

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

func hasDuplicates[T comparable](slice []T) bool {
	seen := make(map[T]struct{}, len(slice))
	for _, v := range slice {
		if _, ok := seen[v]; ok {
			return true
		}
		seen[v] = struct{}{}
	}
	return false
}
