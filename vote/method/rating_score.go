package method

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type RatingScore struct {
	Options           []int              `json:"options"`
	MaxOptionsAmount  dsfetch.Maybe[int] `json:"max_options_amount"`
	MinOptionsAmount  dsfetch.Maybe[int] `json:"min_options_amount"`
	MaxVotesPerOption dsfetch.Maybe[int] `json:"max_votes_per_option"`
	MaxVoteSum        dsfetch.Maybe[int] `json:"max_vote_sum"`
	MinVoteSum        dsfetch.Maybe[int] `json:"min_vote_sum"`
}

func RatingScoreFromDB(configDB dsmodels.PollConfigRatingScore) RatingScore {
	return RatingScore{
		Options:           configDB.OptionIDs,
		MaxOptionsAmount:  maybeZeroIsNull(configDB.MaxOptionsAmount),
		MinOptionsAmount:  maybeZeroIsNull(configDB.MinOptionsAmount),
		MaxVotesPerOption: maybeZeroIsNull(configDB.MaxVotesPerOption),
		MaxVoteSum:        maybeZeroIsNull(configDB.MaxVoteSum),
		MinVoteSum:        maybeZeroIsNull(configDB.MinVoteSum),
	}
}

func RatingScoreFromRequest(config json.RawMessage) (*RatingScore, error) {
	var cfg struct {
		Options           []any              `json:"options"`
		MaxOptionsAmount  dsfetch.Maybe[int] `json:"max_options_amount"`
		MinOptionsAmount  dsfetch.Maybe[int] `json:"min_options_amount"`
		MaxVotesPerOption dsfetch.Maybe[int] `json:"max_votes_per_option"`
		MaxVoteSum        dsfetch.Maybe[int] `json:"max_vote_sum"`
		MinVoteSum        dsfetch.Maybe[int] `json:"min_vote_sum"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, invalidConfigError{}
	}

	// This is only needed for easier testing. In production, the id from the db is used.
	options := make([]int, len(cfg.Options))
	for i := range cfg.Options {
		options[i] = i + 1
	}

	return &RatingScore{
		Options:           options,
		MaxOptionsAmount:  cfg.MaxOptionsAmount,
		MinOptionsAmount:  cfg.MinOptionsAmount,
		MaxVotesPerOption: cfg.MaxVotesPerOption,
		MaxVoteSum:        cfg.MaxVoteSum,
		MinVoteSum:        cfg.MinVoteSum,
	}, nil
}

func (rs RatingScore) Name() string {
	return "rating_score"
}

func ratingScoreSaveConfig(ctx context.Context, tx pgx.Tx, config json.RawMessage) (string, error) {
	rs, err := RatingScoreFromRequest(config)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}

	var cfg struct {
		OneHundredPercentBase string `json:"onehundred_percent_base"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return "", fmt.Errorf("load additional config: %w", err)
	}

	if cfg.OneHundredPercentBase == "" {
		return "", invalidConfig("field onehundred_percent_base is required.")
	}

	var configID int
	sql := `INSERT INTO poll_config_rating_score
	(max_options_amount, min_options_amount, max_votes_per_option, max_vote_sum, min_vote_sum, onehundred_percent_base)
	VALUES ($1, $2, $3, $4, $5, $6)
	RETURNING id;`
	if err := tx.QueryRow(
		ctx,
		sql,
		rs.MaxOptionsAmount,
		rs.MinOptionsAmount,
		rs.MaxVotesPerOption,
		rs.MaxVoteSum,
		rs.MinVoteSum,
		cfg.OneHundredPercentBase,
	).Scan(&configID); err != nil {
		return "", fmt.Errorf("save approval config: %w", err)
	}

	configObjectID := fmt.Sprintf("poll_config_rating_score/%d", configID)

	if err := insertOption(ctx, tx, config, configObjectID); err != nil {
		return "", fmt.Errorf("insert options: %w", err)
	}

	return configObjectID, nil
}

func (rs RatingScore) ValidateBallot(vote json.RawMessage) error {
	var choice map[int]int
	if err := json.Unmarshal(vote, &choice); err != nil {
		return errors.Join(invalidVote("Vote has invalid format"), fmt.Errorf("decoding vote: %w", err))
	}

	if value, set := rs.MaxOptionsAmount.Value(); set && len(choice) > value {
		return invalidVote("too many options")
	}

	if value, set := rs.MinOptionsAmount.Value(); set && len(choice) < value {
		return invalidVote("too few options")
	}

	var sum int
	for option, choice := range choice {
		if !slices.Contains(rs.Options, option) {
			return invalidVote("unknown option id %d", option)
		}

		if choice < 0 {
			return invalidVote("negative value for option")
		}

		if value, set := rs.MaxVotesPerOption.Value(); set {
			if choice > value {
				return invalidVote("too many votes for option")
			}
		}
		sum += choice
	}

	if value, set := rs.MaxVoteSum.Value(); set && sum > value {
		return invalidVote("too many votes")
	}

	if value, set := rs.MinVoteSum.Value(); set && sum < value {
		return invalidVote("too few votes")
	}

	return nil
}

func (rs RatingScore) Result(votes []dsmodels.Ballot) (string, error) {
	return iterateValues(rs, votes, func(value string, weight decimal.Decimal, result map[string]decimal.Decimal) error {
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
