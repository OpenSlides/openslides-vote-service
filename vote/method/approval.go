package method

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type Approval struct {
	AllowAbstain bool `json:"allow_abstain"`
}

func ApprovalFromDB(configDB dsmodels.PollConfigApproval) *Approval {
	return &Approval{
		AllowAbstain: configDB.AllowAbstain,
	}
}

func ApprovalFromRequest(config json.RawMessage) (*Approval, error) {
	var cfg struct {
		AllowAbstain dsfetch.Maybe[bool] `json:"allow_abstain"`
	}
	if len(config) != 0 {
		if err := json.Unmarshal(config, &cfg); err != nil {
			return nil, errors.Join(invalidConfigError{}, err)
		}
	}

	allowAbstain, set := cfg.AllowAbstain.Value()
	if !set {
		allowAbstain = true
	}

	return &Approval{
		AllowAbstain: allowAbstain,
	}, nil
}

func (Approval) Name() string {
	return "approval"
}

func approvalSaveConfig(ctx context.Context, tx pgx.Tx, config json.RawMessage) (string, error) {
	a, err := ApprovalFromRequest(config)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}

	var cfg struct {
		OneHundredPercentBase string `json:"onehundred_percent_base"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return "", fmt.Errorf("load additional config: %w", err)
	}

	var configID int
	sql := `INSERT INTO poll_config_approval (allow_abstain, onehundred_percent_base) VALUES ($1, $2) RETURNING id;`
	if err := tx.QueryRow(ctx, sql, a.AllowAbstain, cfg.OneHundredPercentBase).Scan(&configID); err != nil {
		return "", fmt.Errorf("save approval config: %w", err)
	}

	return fmt.Sprintf("poll_config_approval/%d", configID), nil
}

func (a *Approval) ValidateBallot(ballot json.RawMessage) error {
	switch strings.ToLower(string(ballot)) {
	case `"yes"`, `"no"`:
		return nil
	case `"abstain"`:
		if !a.AllowAbstain {
			return invalidVote("abstain disabled")
		}
		return nil
	default:
		return invalidVote("Unknown value %s", ballot)
	}
}

func (a *Approval) Result(ballots []dsmodels.Ballot) (string, error) {
	return iterateValues(a, ballots, func(value string, weight decimal.Decimal, result map[string]decimal.Decimal) error {
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
