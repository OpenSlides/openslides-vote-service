package method

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// Method is an interface to handle the method of a poll.
type Method interface {
	Name() string
	ValidateBallot(ballot json.RawMessage) error
	Result(votes []dsmodels.Ballot) (string, error)
}

// SaveConfig saves the configuration for a given vote method.
func SaveConfig(ctx context.Context, tx pgx.Tx, method string, config json.RawMessage) (string, error) {
	switch method {
	case Approval{}.Name():
		return approvalSaveConfig(ctx, tx, config)
	case Selection{}.Name():
		return selectionSaveConfig(ctx, tx, config)
	case RatingScore{}.Name():
		return ratingScoreSaveConfig(ctx, tx, config)
	case RatingApproval{}.Name():
		return ratingApprovalSaveConfig(ctx, tx, config)
	default:
		return "", fmt.Errorf("unknown method: %s", method)
	}
}

const (
	keyAbstain = "abstain"
	keyNota    = "nota"
	keyInvalid = "invalid"
)

var reservedOptionNames = []string{keyAbstain, keyNota, keyInvalid}

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
	m Method,
	votes []dsmodels.Ballot,
	fn func(value string, weight decimal.Decimal, result map[string]decimal.Decimal) error,
) (string, error) {
	result := make(map[string]decimal.Decimal)
	invalid := 0
	for _, vote := range votes {
		if err := m.ValidateBallot(json.RawMessage(vote.Value)); err != nil {
			if _, ok := errors.AsType[InvalidBallotError](err); ok {
				invalid++
				continue
			}
			return "", fmt.Errorf("validating vote: %w", err)
		}

		factor := vote.Weight
		if factor.IsZero() {
			factor = decimal.NewFromInt(1)
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

func insertOption(ctx context.Context, tx pgx.Tx, config json.RawMessage, configObjectID string) error {
	var cfg struct {
		Type    string `json:"option_type"`
		Options []any  `json:"options"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}

	if len(cfg.Options) == 0 {
		return invalidConfig("Need at least one value in options")
	}

	for _, option := range cfg.Options {
		str, ok := option.(string)
		if !ok {
			continue
		}
		if slices.Contains(reservedOptionNames, str) {
			return invalidConfig("%s is not allowed as an option", option)
		}
	}

	var sqlColumns string
	var args []any

	switch cfg.Type {
	case "text":
		sqlColumns = `(poll_config_id, weight, text)`
	case "meeting_user":
		sqlColumns = `(poll_config_id, weight, meeting_user_id)`
	default:
		return invalidConfig("unknown option_type %q", cfg.Type)
	}

	for weight, opt := range cfg.Options {
		args = append(args, configObjectID, weight, opt)
	}

	valuePlaceholders := make([]string, len(cfg.Options))
	for i := range cfg.Options {
		valuePlaceholders[i] = fmt.Sprintf("($%d, $%d, $%d)", 3*i+1, 3*i+2, 3*i+3)
	}

	query := fmt.Sprintf(
		"INSERT INTO poll_config_option %s VALUES %s",
		sqlColumns,
		strings.Join(valuePlaceholders, ", "),
	)

	if _, err := tx.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert options: %w", err)
	}

	return nil
}

func maybeZeroIsNull(n int) dsfetch.Maybe[int] {
	if n == 0 {
		return dsfetch.Maybe[int]{}
	}

	return dsfetch.MaybeValue(n)
}

// TODO: Maybe find a way to directly implement this in the maybe type, so pgx
// can understand it.
func maybeNullIsNil(n dsfetch.Maybe[int]) any {
	v, isNull := n.Value()
	if isNull {
		return nil
	}
	return v
}

type invalidConfigError struct {
	msg string
}

func invalidConfig(msg string, a ...any) invalidConfigError {
	return invalidConfigError{msg: fmt.Sprintf(msg, a...)}
}

func (invalidConfigError) Type() string {
	return "invalid_config"
}

func (err invalidConfigError) Error() string {
	if err.msg == "" {
		return "Invalid value for field 'config'"
	}
	return err.msg
}

// InvalidBallotError is returned when the ballot has an invalid format.
type InvalidBallotError struct {
	msg string
}

func (InvalidBallotError) Type() string {
	return "invalid_ballot"
}

func (err InvalidBallotError) Error() string {
	return err.msg
}

func invalidVote(msg string, a ...any) InvalidBallotError {
	return InvalidBallotError{msg: fmt.Sprintf(msg, a...)}
}
