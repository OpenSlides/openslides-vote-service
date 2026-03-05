package vote

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/OpenSlides/openslides-go/datastore/dsfetch"
	"github.com/OpenSlides/openslides-go/datastore/dskey"
	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/OpenSlides/openslides-go/datastore/flow"
	"github.com/OpenSlides/openslides-go/environment"
	"github.com/OpenSlides/openslides-go/perm"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

var envVoteSecretKeyFile = environment.NewVariable("VOTE_SECRET_KEY_FILE", "/run/secrets/vote_secret_key", "Path to the secret key for secret polls.")

// Vote holds the state of the service.
//
// Vote has to be initializes with vote.New().
type Vote struct {
	flow              flow.Flow
	querier           DBQuerier
	gcmForSecretPolls cipher.AEAD
}

// New creates an initializes vote service.
func New(lookup environment.Environmenter, flow flow.Flow, querier DBQuerier) (*Vote, func(context.Context, func(error)), error) {
	key, err := environment.ReadSecret(lookup, envVoteSecretKeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read secret key: %w", err)
	}

	hashedKey := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(hashedKey[:])
	if err != nil {
		return nil, nil, fmt.Errorf("create cipher for secret polls: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("create GCM for secret polls: %w", err)
	}

	v := &Vote{
		flow:              flow,
		querier:           querier,
		gcmForSecretPolls: gcm,
	}

	bg := func(ctx context.Context, errorHandler func(error)) {
		v.flow.Update(ctx, func(changedData map[dskey.Key][]byte, err error) {
			if err != nil {
				errorHandler(err)
			}

			// This listens on the message bus to see, if a poll got started. If
			// so, it preloads its data. This is only relevant, if a poll gets
			// started on another instance.
			for key, value := range changedData {
				if key.CollectionField() == "poll/state" && string(value) == `"started"` {
					poll, err := dsmodels.New(v.flow).Poll(key.ID()).First(ctx)
					if err != nil {
						errorHandler(fmt.Errorf("Error fetching poll for preload: %w", err))
						continue
					}
					if err := Preload(ctx, dsfetch.New(v.flow), poll.ID, poll.MeetingID); err != nil {
						errorHandler(fmt.Errorf("Error preloading poll: %w", err))
						continue
					}
				}
			}
		})
	}

	return v, bg, nil
}

// Create create a poll, returning the poll id.
func (v *Vote) Create(ctx context.Context, requestUserID int, r io.Reader) (int, error) {
	electronicVotingEnabled, err := dsfetch.New(v.flow).Organization_EnableElectronicVoting(1).Value(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetch organization/1/enable_electronic_voting: %w", err)
	}

	ci, err := parseCreateInput(r, electronicVotingEnabled)
	if err != nil {
		return 0, fmt.Errorf("parsing input: %w", err)
	}

	if err := canManagePoll(ctx, v.flow, ci.MeetingID, ci.ContentObjectID, requestUserID); err != nil {
		return 0, fmt.Errorf("check permissions: %w", err)
	}

	tx, err := v.querier.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	configID, err := saveConfig(ctx, tx, ci.Method, ci.Config)
	if err != nil {
		return 0, fmt.Errorf("save poll config: %w", err)
	}

	state := "created"
	if ci.Visibility == "manually" {
		state = "finished"
	}

	sql := `INSERT INTO poll
		(title, config_id, visibility, state, content_object_id, meeting_id, result, published, allow_vote_split)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id;`

	var newID int
	if err := tx.QueryRow(
		ctx,
		sql,
		ci.Title,
		configID,
		ci.Visibility,
		state,
		ci.ContentObjectID,
		ci.MeetingID,
		string(ci.Result),
		ci.Published,
		ci.AllowVoteSplit,
	).Scan(&newID); err != nil {
		return 0, fmt.Errorf("save poll: %w", err)
	}

	if len(ci.EntitledGroupIDs) > 0 {
		placeholders := make([]string, len(ci.EntitledGroupIDs))
		args := make([]any, len(ci.EntitledGroupIDs)*2)

		for i, groupID := range ci.EntitledGroupIDs {
			placeholders[i] = fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2)
			args[i*2] = groupID
			args[i*2+1] = newID
		}

		groupSQL := fmt.Sprintf(
			"INSERT INTO nm_group_poll_ids_poll_t (group_id, poll_id) VALUES %s",
			strings.Join(placeholders, ", "),
		)

		if _, err := tx.Exec(ctx, groupSQL, args...); err != nil {
			return 0, fmt.Errorf("insert group-poll relations: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return newID, nil
}

// TODO: Move this function into the methods interface.
func saveConfig(ctx context.Context, tx pgx.Tx, method string, config json.RawMessage) (string, error) {
	// deleteStatements := []string{
	// 	`DELETE FROM poll_config_approval WHERE poll_id = $1`,
	// 	`DELETE FROM poll_config_selection WHERE poll_id = $1`,
	// 	`DELETE FROM poll_config_rating_score WHERE poll_id = $1`,
	// 	`DELETE FROM poll_config_rating_approval WHERE poll_id = $1`,
	// }
	// for _, sql := range deleteStatements {
	// 	if _, err := tx.Exec(ctx, sql, pollID); err != nil {
	// 		return fmt.Errorf("remove old config entries for poll %d: %w", pollID, err)
	// 	}
	// }

	var configObjectID string
	switch method {
	case "approval":
		var cfg methodApprovalConfig
		if err := json.Unmarshal(config, &cfg); err != nil {
			return "", errors.Join(
				MessageError(ErrInvalid, "Invalid value for field 'config'"),
				fmt.Errorf("parsing approval config: %w", err),
			)
		}

		allowAbstain, set := cfg.AllowAbstain.Value()
		if !set {
			allowAbstain = true
		}

		var configID int
		sql := `INSERT INTO poll_config_approval (allow_abstain) VALUES ($1) RETURNING id;`
		if err := tx.QueryRow(ctx, sql, allowAbstain).Scan(&configID); err != nil {
			return "", fmt.Errorf("save approval config: %w", err)
		}

		configObjectID = fmt.Sprintf("poll_config_approval/%d", configID)

	case "selection":
		var cfg struct {
			MaxOptionsAmount int  `json:"max_options_amount"`
			MinOptionsAmount int  `json:"min_options_amount"`
			AllowNota        bool `json:"allow_nota"`
		}
		if err := json.Unmarshal(config, &cfg); err != nil {
			return "", errors.Join(
				fmt.Errorf("parsing selection config: %w", err),
				MessageError(ErrInvalid, "Invalid value for field 'config'"),
			)
		}

		var configID int
		sql := `INSERT INTO poll_config_selection
		(max_options_amount, min_options_amount, allow_nota)
		VALUES ($1, $2, $3)
		RETURNING id;`
		if err := tx.QueryRow(ctx, sql, cfg.MaxOptionsAmount, cfg.MinOptionsAmount, cfg.AllowNota).Scan(&configID); err != nil {
			return "", fmt.Errorf("save approval config: %w", err)
		}

		configObjectID = fmt.Sprintf("poll_config_selection/%d", configID)

		if err := insertOption(ctx, tx, config, configObjectID); err != nil {
			return "", fmt.Errorf("insert options: %w", err)
		}

	case "rating_score":
		var cfg struct {
			MaxOptionsAmount  int `json:"max_options_amount"`
			MinOptionsAmount  int `json:"min_options_amount"`
			MaxVotesPerOption int `json:"max_votes_per_option"`
			MaxVoteSum        int `json:"max_vote_sum"`
			MinVoteSum        int `json:"min_vote_sum"`
		}
		if err := json.Unmarshal(config, &cfg); err != nil {
			return "", errors.Join(
				fmt.Errorf("parsing rating score config: %w", err),
				MessageError(ErrInvalid, "Invalid value for field 'config'"),
			)
		}

		var configID int
		sql := `INSERT INTO poll_config_rating_score
		(max_options_amount, min_options_amount, max_votes_per_option, max_vote_sum, min_vote_sum)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id;`
		if err := tx.QueryRow(
			ctx,
			sql,
			cfg.MaxOptionsAmount,
			cfg.MinOptionsAmount,
			cfg.MaxVotesPerOption,
			cfg.MaxVoteSum,
			cfg.MinVoteSum,
		).Scan(&configID); err != nil {
			return "", fmt.Errorf("save approval config: %w", err)
		}

		configObjectID = fmt.Sprintf("poll_config_rating_score/%d", configID)

		if err := insertOption(ctx, tx, config, configObjectID); err != nil {
			return "", fmt.Errorf("insert options: %w", err)
		}

	case "rating_approval":
		var cfg struct {
			MaxOptionsAmount int                 `json:"max_options_amount"`
			MinOptionsAmount int                 `json:"min_options_amount"`
			AllowAbstain     dsfetch.Maybe[bool] `json:"allow_abstain"`
		}
		if err := json.Unmarshal(config, &cfg); err != nil {
			return "", errors.Join(
				fmt.Errorf("parsing rating approval config: %w", err),
				MessageError(ErrInvalid, "Invalid value for field 'config'"),
			)
		}

		allowAbstain, set := cfg.AllowAbstain.Value()
		if !set {
			allowAbstain = true
		}

		var configID int
		sql := `INSERT INTO poll_config_rating_approval
		(max_options_amount, min_options_amount, allow_abstain)
		VALUES ($1, $2, $3)
		RETURNING id;`
		if err := tx.QueryRow(
			ctx,
			sql,
			cfg.MaxOptionsAmount,
			cfg.MinOptionsAmount,
			allowAbstain,
		).Scan(&configID); err != nil {
			return "", fmt.Errorf("save approval config: %w", err)
		}

		configObjectID = fmt.Sprintf("poll_config_rating_approval/%d", configID)

		if err := insertOption(ctx, tx, config, configObjectID); err != nil {
			return "", fmt.Errorf("insert options: %w", err)
		}
	}

	// sql := `UPDATE poll SET config_id = $2 WHERE id = $1`
	// if _, err := tx.Exec(ctx, sql, pollID, configObjectID); err != nil {
	// 	return fmt.Errorf("update config value of poll: %w", err)
	// }

	return configObjectID, nil
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
		return MessageError(ErrInvalid, "Need at least value in options")
	}

	for _, option := range cfg.Options {
		str, ok := option.(string)
		if !ok {
			continue
		}
		if slices.Contains(reservedOptionNames, str) {
			return MessageErrorf(ErrInternal, "%s is not allowed as an option", option)
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
		return MessageErrorf(ErrInvalid, "unknown option_type %q", cfg.Type)
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

type createInput struct {
	Title            string          `json:"title"`
	ContentObjectID  string          `json:"content_object_id"`
	MeetingID        int             `json:"meeting_id"`
	Method           string          `json:"method"`
	Config           json.RawMessage `json:"config"`
	Visibility       string          `json:"visibility"`
	EntitledGroupIDs []int           `json:"entitled_group_ids"`
	Published        bool            `json:"published"`
	Result           json.RawMessage `json:"result"`
	AllowVoteSplit   bool            `json:"allow_vote_split"`
}

func parseCreateInput(r io.Reader, electronicVotingEnabled bool) (createInput, error) {
	var ci createInput
	if err := json.NewDecoder(r).Decode(&ci); err != nil {
		return createInput{}, MessageError(ErrInvalid, "Invalid request body.")
	}

	if ci.Title == "" {
		return createInput{}, MessageError(ErrInvalid, "Field 'title' can not be empty")
	}

	if ci.ContentObjectID == "" {
		return createInput{}, MessageError(ErrInvalid, "Field 'content_object_id' can not be empty")
	}

	if ci.MeetingID == 0 {
		return createInput{}, MessageError(ErrInvalid, "Field 'meeting_id' can not be empty")
	}

	if ci.Method == "" {
		return createInput{}, MessageError(ErrInvalid, "Field 'method' can not be empty")
	}

	if ci.Config == nil {
		return createInput{}, MessageError(ErrInvalid, "Field 'config' can not be empty")
	}

	if ci.Visibility == "" {
		return createInput{}, MessageError(ErrInvalid, "Field 'visibility' can not be empty")
	}

	if ci.Visibility == "secret" && ci.AllowVoteSplit {
		return createInput{}, MessageError(ErrInvalid, "Vote splitting is not allowed for secret polls")
	}

	switch ci.Visibility {
	case "manually":
		if len(ci.EntitledGroupIDs) > 0 {
			return createInput{}, MessageError(ErrInvalid, "Entitled Group IDs can not be set when visibility is set to manually")
		}

	default:
		if !electronicVotingEnabled {
			return createInput{}, MessageError(ErrNotAllowed, "Electronic voting is not enabled. Only polls with visibility set to manually are allowed.")
		}

		if ci.Result != nil {
			return createInput{}, MessageError(ErrInvalid, "Result can only be set when visibility is set to manually")
		}
	}

	return ci, nil
}

// Update changes a poll.
func (v *Vote) Update(ctx context.Context, pollID int, requestUserID int, r io.Reader) error {
	poll, err := fetchPoll(ctx, v.flow, pollID)
	if err != nil {
		return fmt.Errorf("fetching poll: %w", err)
	}

	if err := canManagePoll(ctx, v.flow, poll.MeetingID, poll.ContentObjectID, requestUserID); err != nil {
		return fmt.Errorf("check permissions: %w", err)
	}

	electronicVotingEnabled, err := dsfetch.New(v.flow).Organization_EnableElectronicVoting(1).Value(ctx)
	if err != nil {
		return fmt.Errorf("fetch organization/1/enable_electronic_voting: %w", err)
	}

	ui, err := parseUpdateInput(r, poll, electronicVotingEnabled)
	if err != nil {
		return fmt.Errorf("parse update body: %w", err)
	}

	tx, err := v.querier.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	sql, values := ui.query(pollID)
	if len(values) > 0 {
		if _, err := tx.Exec(ctx, sql, values...); err != nil {
			return fmt.Errorf("update poll: %w", err)
		}
	}

	if ui.Method != "" || ui.Config != nil {
		method := pollMethod(poll)
		if ui.Method != "" {
			method = ui.Method
		}

		// TODO: Remove old config
		newConfigID, err := saveConfig(ctx, tx, method, ui.Config)
		if err != nil {
			return fmt.Errorf("save poll config: %w", err)
		}

		// TODO: Update poll with new config id
		_ = newConfigID
	}

	if len(ui.EntitledGroupIDs) > 0 {
		sql := "DELETE FROM nm_group_poll_ids_poll_t WHERE poll_id = $1"
		if _, err := tx.Exec(ctx, sql, pollID); err != nil {
			return fmt.Errorf("deleting existing group associations: %w", err)
		}

		placeholders := make([]string, len(ui.EntitledGroupIDs))
		args := make([]any, len(ui.EntitledGroupIDs)*2)

		for i, groupID := range ui.EntitledGroupIDs {
			placeholders[i] = fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2)
			args[i*2] = groupID
			args[i*2+1] = poll.ID
		}

		groupSQL := fmt.Sprintf(
			"INSERT INTO nm_group_poll_ids_poll_t (group_id, poll_id) VALUES %s",
			strings.Join(placeholders, ", "),
		)

		if _, err := tx.Exec(ctx, groupSQL, args...); err != nil {
			return fmt.Errorf("insert group-poll relations: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

type updateInput struct {
	Title            string              `json:"title"`
	Method           string              `json:"method"`
	Config           json.RawMessage     `json:"config"`
	Visibility       string              `json:"visibility"`
	EntitledGroupIDs []int               `json:"entitled_group_ids"`
	Published        dsfetch.Maybe[bool] `json:"published"`
	Result           json.RawMessage     `json:"result"`
	AllowVoteSplit   dsfetch.Maybe[bool] `json:"allow_vote_split"`
}

func parseUpdateInput(r io.Reader, poll dsmodels.Poll, electronicVotingEnabled bool) (updateInput, error) {
	var ui updateInput
	if err := json.NewDecoder(r).Decode(&ui); err != nil {
		return updateInput{}, fmt.Errorf("decoding update input: %w", err)
	}

	if poll.Visibility == "manually" {
		if len(ui.EntitledGroupIDs) > 0 {
			return updateInput{}, MessageError(ErrNotAllowed, "Entitled Group IDs can not be set when visibility is set to manually")
		}
		return ui, nil
	}

	if ui.Visibility == "manually" {
		return updateInput{}, MessageError(ErrNotAllowed, "A poll can not be changed manually")
	}

	if poll.State != "created" {
		if ui.Method != "" {
			return updateInput{}, MessageError(ErrNotAllowed, "method can only be changed before the poll has started")
		}

		if ui.Config != nil {
			return updateInput{}, MessageError(ErrNotAllowed, "config can only be changed before the poll has started")
		}

		if ui.Visibility != "" {
			return updateInput{}, MessageError(ErrNotAllowed, "visibility can only be changed before the poll has started")
		}

		if ui.EntitledGroupIDs != nil {
			return updateInput{}, MessageError(ErrNotAllowed, "entitled group ids can only be changed before the poll has started")
		}

		if !ui.AllowVoteSplit.Null() {
			return updateInput{}, MessageError(ErrNotAllowed, "allow vote split can only be changed before the poll has started")
		}
	}

	if !electronicVotingEnabled {
		return updateInput{}, MessageError(ErrNotAllowed, "Electronic voting is not enabled. Only polls with visibility set to manually are allowed.")
	}

	if ui.Result != nil {
		return updateInput{}, MessageError(ErrNotAllowed, "Result can only be set when visibility is set to manually")
	}

	return ui, nil
}

func (ui updateInput) query(pollID int) (string, []any) {
	var setParts []string
	var args []any
	argIndex := 1

	if ui.Title != "" {
		setParts = append(setParts, fmt.Sprintf("title = $%d", argIndex))
		args = append(args, ui.Title)
		argIndex++
	}

	if ui.Method != "" {
		setParts = append(setParts, fmt.Sprintf("method = $%d", argIndex))
		args = append(args, ui.Method)
		argIndex++
	}

	if ui.Config != nil {
		setParts = append(setParts, fmt.Sprintf("config = $%d", argIndex))
		args = append(args, string(ui.Config))
		argIndex++
	}

	if ui.Visibility != "" {
		setParts = append(setParts, fmt.Sprintf("visibility = $%d", argIndex))
		args = append(args, ui.Visibility)
		argIndex++
	}

	if published, hasValue := ui.Published.Value(); hasValue {
		setParts = append(setParts, fmt.Sprintf("published = $%d", argIndex))
		args = append(args, published)
		argIndex++
	}

	if ui.Result != nil {
		setParts = append(setParts, fmt.Sprintf("result = $%d", argIndex))
		args = append(args, string(ui.Result))
		argIndex++
	}

	if allowVoteSplit, hasValue := ui.AllowVoteSplit.Value(); hasValue {
		setParts = append(setParts, fmt.Sprintf("allow_vote_split = $%d", argIndex))
		args = append(args, allowVoteSplit)
		argIndex++
	}

	if len(setParts) == 0 {
		return "", nil
	}

	query := fmt.Sprintf("UPDATE poll SET %s WHERE id = $%d",
		strings.Join(setParts, ", "),
		argIndex)

	args = append(args, pollID)

	return query, args
}

// Delete removes a poll.
func (v *Vote) Delete(ctx context.Context, pollID int, requestUserID int) error {
	poll, err := fetchPoll(ctx, v.flow, pollID)
	if err != nil {
		return fmt.Errorf("fetching poll: %w", err)
	}

	if err := canManagePoll(ctx, v.flow, poll.MeetingID, poll.ContentObjectID, requestUserID); err != nil {
		return fmt.Errorf("check permissions: %w", err)
	}

	tx, err := v.querier.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	deleteStatements := []string{
		`DELETE FROM poll_config_approval WHERE poll_id = $1`,
		`DELETE FROM poll_config_selection WHERE poll_id = $1`,
		`DELETE FROM poll_config_rating_score WHERE poll_id = $1`,
		`DELETE FROM poll_config_rating_approval WHERE poll_id = $1`,
	}
	for _, sql := range deleteStatements {
		if _, err := tx.Exec(ctx, sql, pollID); err != nil {
			return fmt.Errorf("remove old config entries for poll %d: %w", pollID, err)
		}
	}

	sql := `DELETE FROM poll WHERE id = $1;`
	if _, err := tx.Exec(ctx, sql, pollID); err != nil {
		return fmt.Errorf("delete poll: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// EncodeConfig encodes the configuration of a poll into a string.
func (v *Vote) EncodeConfig(ctx context.Context, poll dsmodels.Poll) (string, error) {
	configCollection, configIDStr, found := strings.Cut(poll.ConfigID, "/")
	if !found {
		return "", fmt.Errorf("poll %d has an invalid config_id: %s", poll.ID, poll.ConfigID)
	}

	configID, err := strconv.Atoi(configIDStr)
	if err != nil {
		return "", fmt.Errorf("poll %d ha san invalid config_id. Second part is not a number: %s", poll.ID, poll.ConfigID)
	}

	dsm := dsmodels.New(v.flow)
	var config any

	switch configCollection {
	case "poll_config_approval":
		configDB, err := dsm.PollConfigApproval(configID).First(ctx)
		if err != nil {
			return "", fmt.Errorf("fetching poll_config_approval: %w", err)
		}

		config = methodApprovalConfig{
			AllowAbstain: dsfetch.MaybeValue(configDB.AllowAbstain),
		}

	case "poll_config_selection":
		configDB, err := dsm.PollConfigSelection(configID).First(ctx)
		if err != nil {
			return "", fmt.Errorf("fetching poll_config_selection: %w", err)
		}

		config = methodSelectionConfig{
			Options:          configDB.OptionIDs,
			MaxOptionsAmount: maybeZeroIsNull(configDB.MaxOptionsAmount),
			MinOptionsAmount: maybeZeroIsNull(configDB.MinOptionsAmount),
			AllowNota:        configDB.AllowNota,
		}

	case "poll_config_rating_score":
		configDB, err := dsm.PollConfigRatingScore(configID).First(ctx)
		if err != nil {
			return "", fmt.Errorf("fetching poll_config_rating_score: %w", err)
		}

		config = methodRatingScoreConfig{
			Options:           configDB.OptionIDs,
			MaxOptionsAmount:  maybeZeroIsNull(configDB.MaxOptionsAmount),
			MinOptionsAmount:  maybeZeroIsNull(configDB.MinOptionsAmount),
			MaxVotesPerOption: maybeZeroIsNull(configDB.MaxVotesPerOption),
			MaxVoteSum:        maybeZeroIsNull(configDB.MaxVoteSum),
			MinVoteSum:        maybeZeroIsNull(configDB.MinVoteSum),
		}

	case "poll_config_rating_approval":
		configDB, err := dsm.PollConfigRatingApproval(configID).First(ctx)
		if err != nil {
			return "", fmt.Errorf("fetching poll_config_rating_approval: %w", err)
		}

		config = methodRatingApprovalConfig{
			Options:          configDB.OptionIDs,
			MaxOptionsAmount: maybeZeroIsNull(configDB.MaxOptionsAmount),
			MinOptionsAmount: maybeZeroIsNull(configDB.MinOptionsAmount),
			AllowAbstain:     dsfetch.MaybeValue(configDB.AllowAbstain),
		}

	default:
		panic(fmt.Sprintf("poll %d has an unknown poll config: %s", poll.ID, poll.ConfigID))
	}

	encoded, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("encoding config: %w", err)
	}

	return string(encoded), nil
}

func pollMethod(poll dsmodels.Poll) string {
	configCollection, _, found := strings.Cut(poll.ConfigID, "/")
	if !found {
		panic(fmt.Sprintf("poll %d has an invalid config_id: %s", poll.ID, poll.ConfigID))
	}

	switch configCollection {
	case "poll_config_approval":
		return "approval"
	case "poll_config_selection":
		return "selection"
	case "poll_config_rating_score":
		return "rating_score"
	case "poll_config_rating_approval":
		return "rating_approval"
	default:
		panic(fmt.Sprintf("poll %d has an unknown poll config: %s", poll.ID, poll.ConfigID))
	}
}

// ValidateBallot checks, if a vote is invalid.
func ValidateBallot(method string, config string, vote json.RawMessage) error {
	switch method {
	case methodApproval{}.Name():
		return methodApproval{}.ValidateVote(config, vote)
	case methodSelection{}.Name():
		return methodSelection{}.ValidateVote(config, vote)
	case methodRatingScore{}.Name():
		return methodRatingScore{}.ValidateVote(config, vote)
	case methodRatingApproval{}.Name():
		return methodRatingApproval{}.ValidateVote(config, vote)
	default:
		return fmt.Errorf("unknown poll method: %s", method)
	}
}

func ballotsFromSplitted(method string, config string, ballot dsmodels.Ballot, splitted map[decimal.Decimal]json.RawMessage) []dsmodels.Ballot {
	var fromThisBallot []dsmodels.Ballot
	for splitWeight, splitValue := range splitted {
		if err := ValidateBallot(method, config, splitValue); err != nil {
			return []dsmodels.Ballot{ballot}
		}

		fromThisBallot = append(fromThisBallot, dsmodels.Ballot{
			PollID:                   ballot.PollID,
			Weight:                   splitWeight,
			Value:                    string(splitValue),
			ActingMeetingUserID:      ballot.ActingMeetingUserID,
			RepresentedMeetingUserID: ballot.RepresentedMeetingUserID,
			Split:                    true,
		})
	}
	return fromThisBallot
}

func fetchPoll(ctx context.Context, getter flow.Getter, pollID int) (dsmodels.Poll, error) {
	ds := dsmodels.New(getter)
	poll, err := ds.Poll(pollID).First(ctx)
	if err != nil {
		var doesNotExist dsfetch.DoesNotExistError
		if errors.As(err, &doesNotExist) {
			return dsmodels.Poll{}, MessageErrorf(ErrNotExists, "Poll %d does not exist", pollID)
		}
		return dsmodels.Poll{}, fmt.Errorf("loading poll %d: %w", pollID, err)
	}

	return poll, nil
}

func canManagePoll(ctx context.Context, getter flow.Getter, meetingID int, contentObjectID string, userID int) error {
	collection, _, found := strings.Cut(contentObjectID, "/")
	if !found {
		return fmt.Errorf("invalid content object id: %s", contentObjectID)
	}

	var requiredPerm perm.TPermission
	switch collection {
	case "motion":
		requiredPerm = perm.MotionCanManagePolls
	case "assignment":
		requiredPerm = perm.AssignmentCanManagePolls
	case "topic":
		requiredPerm = perm.PollCanManage
	default:
		return fmt.Errorf(
			"invalid content object id %s, only motion, assignment or topic allowed",
			contentObjectID,
		)
	}

	userPerms, err := perm.New(ctx, dsfetch.New(getter), userID, meetingID)
	if err != nil {
		return fmt.Errorf("calculate user permissions: %w", err)
	}

	if !userPerms.Has(requiredPerm) {
		return MessageError(ErrNotAllowed, "You are not allowed to manage a poll")
	}

	return nil
}

// Preload loads all data in the cache, that is needed later for the vote
// requests.
func Preload(ctx context.Context, flow flow.Getter, pollID int, meetingID int) error {
	ds := dsmodels.New(flow)
	var dummyBool bool
	ds.Meeting_UsersEnableVoteWeight(meetingID).Lazy(&dummyBool)
	ds.Meeting_UsersEnableVoteDelegations(meetingID).Lazy(&dummyBool)
	ds.Meeting_UsersForbidDelegatorToVote(meetingID).Lazy(&dummyBool)

	q := ds.Poll(pollID)
	q = q.Preload(q.EntitledGroupList().MeetingUserList().User())
	q = q.Preload(q.EntitledGroupList().MeetingUserList().VoteDelegatedTo().User())
	poll, err := q.First(ctx)
	if err != nil {
		return fmt.Errorf("fetch preload data: %w", err)
	}

	configCollection, configIDStr, found := strings.Cut(poll.ConfigID, "/")
	if !found {
		return fmt.Errorf("invalid value in configID: %s", poll.ConfigID)
	}

	configID, err := strconv.Atoi(configIDStr)
	if err != nil {
		return fmt.Errorf("invalid value in configID. Second part has to be an int: %s", poll.ConfigID)
	}

	switch configCollection {
	case "poll_config_approval":
		_, err := ds.PollConfigApproval(configID).First(ctx)
		if err != nil {
			return fmt.Errorf("fetch poll config approval: %w", err)
		}

	case "poll_config_selection":
		_, err := ds.PollConfigSelection(configID).First(ctx)
		if err != nil {
			return fmt.Errorf("fetch poll config selection: %w", err)
		}

	case "poll_config_rating_score":
		_, err := ds.PollConfigRatingScore(configID).First(ctx)
		if err != nil {
			return fmt.Errorf("fetch poll config rating score: %w", err)
		}

	case "poll_config_rating_approval":
		_, err := ds.PollConfigRatingApproval(configID).First(ctx)
		if err != nil {
			return fmt.Errorf("fetch poll config rating approval: %w", err)
		}

	default:
		return fmt.Errorf("invalid config collection. Unknown method: %s", configCollection)

	}

	return nil
}

// DBQuerier is either a pgx-connection or a pgx-pool.
type DBQuerier interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func maybeZeroIsNull(n int) dsfetch.Maybe[int] {
	if n == 0 {
		return dsfetch.Maybe[int]{}
	}

	return dsfetch.MaybeValue(n)
}
