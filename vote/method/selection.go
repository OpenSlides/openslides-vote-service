package method

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type Selection struct {
	Options          []int              `json:"options"`
	MaxOptionsAmount dsfetch.Maybe[int] `json:"max_options_amount"`
	MinOptionsAmount dsfetch.Maybe[int] `json:"min_options_amount"`
	AllowNota        bool               `json:"allow_nota"`
}

func SelectionFromDB(configDB dsmodels.PollConfigSelection) Selection {
	return Selection{
		Options:          configDB.OptionIDs,
		MaxOptionsAmount: maybeZeroIsNull(configDB.MaxOptionsAmount),
		MinOptionsAmount: maybeZeroIsNull(configDB.MinOptionsAmount),
		AllowNota:        configDB.AllowNota,
	}
}

func SelectionFromRequest(config json.RawMessage) (*Selection, error) {
	var cfg struct {
		Options          []any              `json:"options"`
		MaxOptionsAmount dsfetch.Maybe[int] `json:"max_options_amount"`
		MinOptionsAmount dsfetch.Maybe[int] `json:"min_options_amount"`
		AllowNota        bool               `json:"allow_nota"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, invalidConfigError{}
	}

	// This is only needed for easier testing. In production, the id from the db is used.
	options := make([]int, len(cfg.Options))
	for i := range cfg.Options {
		options[i] = i + 1
	}

	return &Selection{
		Options:          options,
		MaxOptionsAmount: cfg.MaxOptionsAmount,
		MinOptionsAmount: cfg.MinOptionsAmount,
		AllowNota:        cfg.AllowNota,
	}, nil
}

func (s Selection) Name() string {
	return "selection"
}

func selectionSaveConfig(ctx context.Context, tx pgx.Tx, config json.RawMessage) (string, error) {
	s, err := SelectionFromRequest(config)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}

	var cfg struct {
		StrikeOut             bool   `json:"strike_out"`
		OneHundredPercentBase string `json:"onehundred_percent_base"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return "", fmt.Errorf("load additional config: %w", err)
	}

	if cfg.OneHundredPercentBase == "" {
		return "", invalidConfig("field onehundred_percent_base is required.")
	}

	var configID int
	sql := `INSERT INTO poll_config_selection
	(max_options_amount, min_options_amount, allow_nota, strike_out, onehundred_percent_base)
	VALUES ($1, $2, $3, $4, $5)
	RETURNING id;`
	if err := tx.QueryRow(
		ctx,
		sql,
		s.MaxOptionsAmount,
		s.MinOptionsAmount,
		s.AllowNota,
		cfg.StrikeOut,
		cfg.OneHundredPercentBase,
	).Scan(&configID); err != nil {
		return "", fmt.Errorf("save approval config: %w", err)
	}

	configObjectID := fmt.Sprintf("poll_config_selection/%d", configID)

	if err := insertOption(ctx, tx, config, configObjectID); err != nil {
		return "", fmt.Errorf("insert options: %w", err)
	}

	return configObjectID, nil
}

func (s Selection) ValidateBallot(vote json.RawMessage) error {
	var choice []int
	if err := json.Unmarshal(vote, &choice); err != nil {
		if s.AllowNota && strings.ToLower(string(vote)) == `"nota"` {
			return nil
		}
		return errors.Join(invalidVote("Vote has invalid format"), fmt.Errorf("decoding vote: %w", err))
	}

	if hasDuplicates(choice) {
		return invalidVote("douplicate entries in vote")
	}

	if value, set := s.MaxOptionsAmount.Value(); set && len(choice) > value {
		return invalidVote("too many options")
	}

	if value, set := s.MinOptionsAmount.Value(); set && len(choice) < value {
		return invalidVote("too few options")
	}
	for _, option := range choice {
		if !slices.Contains(s.Options, option) {
			return invalidVote("unknown option id %d", option)
		}
	}

	return nil
}

func (s Selection) Result(votes []dsmodels.Ballot) (string, error) {
	return iterateValues(s, votes, func(value string, weight decimal.Decimal, result map[string]decimal.Decimal) error {
		var votedOptions []int
		if err := json.Unmarshal([]byte(value), &votedOptions); err != nil {
			if s.AllowNota && strings.ToLower(value) == `"nota"` {
				result[keyNota] = result[keyNota].Add(weight)
				return nil
			}
			return fmt.Errorf("invalid options `%s`: %w", value, err)
		}

		for _, votedOption := range votedOptions {
			result[strconv.Itoa(votedOption)] = result[strconv.Itoa(votedOption)].Add(weight)
		}

		if len(votedOptions) == 0 {
			result[keyAbstain] = result[keyAbstain].Add(weight)
		}

		return nil
	})
}
