package vote

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"

	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
	"github.com/shopspring/decimal"
)

// methodSTVScottish implements the Single Transferable Vote, a type of
// ranked-choice voting that is used for electing a group of candidates, as it
// was enacted in Scotland for local elections in 2007.
//
// A plain explanation is found here: https://blog.opavote.com/2016/11/plain-english-explanation-of-scottish.html
//
// The Scottish Local Government Elections Order 2007 can be found here: https://www.legislation.gov.uk/ssi/2007/42/contents/made
type methodSTVScottish struct{}

type methodSTVScottishConfig struct {
	Posts int `json:"posts"`
}

type methodSTVScottishConfigWithOptions struct {
	methodSTVScottishConfig
	Options []int `json:"options"`
}

func (m methodSTVScottish) Name() string {
	return "stv_scottish"
}

func (m methodSTVScottish) ValidateVote(config string, rawVote json.RawMessage) error {
	var cfg methodSTVScottishConfigWithOptions
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	var vote []int
	if err := json.Unmarshal(rawVote, &vote); err != nil {
		return errors.Join(MessageError(ErrInvalid, "Vote has invalid format, must be JSON"), fmt.Errorf("decoding vote: %w", err))
	}

	for _, val := range vote {
		if !slices.Contains(cfg.Options, val) {
			return MessageErrorf(ErrInvalid, "Unknown option %d", val)
		}
	}

	if hasDuplicates(vote) {
		return MessageError(ErrInvalid, "Vote has duplicates")
	}

	return nil
}

// resultSTVScottish represents the result after calculating all votes.
type resultSTVScottish struct {
	Invalid int             `json:"invalid"`
	Quota   decimal.Decimal `json:"quota"`
	Elected []int           `json:"elected"`
	Stages  []stage         `json:"stages"`
}

type stage map[int]optionResult

type optionResult struct {
	Votes  decimal.Decimal `json:"votes"`
	Status string          `json:"status"`
}

type validVote struct {
	options []int
	weight  decimal.Decimal
}

const elected = "elected"
const excluded = "excluded"
const continuing = "continuing"

func (m methodSTVScottish) Result(config string, votes []dsmodels.Ballot) (string, error) {
	// Check config
	var cfg methodSTVScottishConfigWithOptions
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return "", fmt.Errorf("invalid configuration: %w", err)
	}
	if err := validateConfig(cfg); err != nil {
		return "", fmt.Errorf("invalid configuration: %w", err)
	}

	// Strip of invalid or empty votes
	validVotes := make([]validVote, 0, len(votes))
	for _, vote := range votes {
		if err := m.ValidateVote(config, json.RawMessage(vote.Value)); err != nil {
			break
		}
		var votedOptions []int
		if err := json.Unmarshal([]byte(vote.Value), &votedOptions); err != nil {
			break
		}
		if len(votedOptions) == 0 {
			break
		}
		validVotes = append(validVotes, validVote{options: votedOptions, weight: decimal.NewFromInt(1)})
	}

	// Initialize result and variable for excluded options (candidates)
	var result resultSTVScottish
	result.Invalid = len(votes) - len(validVotes)
	q := (len(validVotes) / (cfg.Posts + 1)) + 1
	result.Quota = decimal.NewFromInt(int64(q))

	// Setup slice of continuing options (candidates) and shuffle them
	cont := make([]int, len(cfg.Options))
	copy(cont, cfg.Options)
	rand.Shuffle(len(cont), func(i, j int) {
		cont[i], cont[j] = cont[j], cont[i]
	})

	// Setup first stage
	newStage := make(stage)
	for _, opt := range cfg.Options {
		newStage[opt] = optionResult{Votes: decimal.NewFromInt(0), Status: continuing}
	}

	// Run stages
	res := resultHelper(result, cfg.Posts, cont, validVotes, newStage)

	// Prepare final result
	finalResult, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("unable to marshal result: %w", err)
	}
	return string(finalResult), nil
}

func resultHelper(result resultSTVScottish, vacancies int, cont []int, validVotes []validVote, currentStage stage) resultSTVScottish {
	// If there are no vacancies we are done.
	if vacancies == 0 {
		return result
	}

	// Check if the number of continuing candidates is equal to the number of vacancies remaining unfilled.
	// If yes, the continuing candidates are deemed to be elected. No further transfer shall be made.
	if len(cont) == vacancies {
		for _, opt := range cont {
			v := result.Stages[len(result.Stages)-1][opt].Votes
			currentStage[opt] = optionResult{Votes: v, Status: elected}
		}
		result.Elected = slices.Concat(result.Elected, cont)
		result.Stages = append(result.Stages, currentStage)
		return result
	}

	// Count votes
	for _, vV := range validVotes {
		opt := vV.options[0]
		currentStage[opt] = optionResult{Votes: currentStage[opt].Votes.Add(vV.weight), Status: continuing}
	}

	// Who has maximum or minimum? Tie breaking is done according to the number
	// of votes at the end of the most recently preceding stage of the count at
	// which they had an unequal number of votes. If the number of votes
	// credited to those candidates was equal at all stages, tie breaking is
	// done with respect of the previously randomize options order.
	//
	// We do this by shuffling the continuing slice at the beginning and then
	// sorting the continuing slice in every stage but keep the order from the
	// beginning in case of a tie.
	slices.SortStableFunc(cont, func(a int, b int) int {
		return currentStage[b].Votes.Cmp(currentStage[a].Votes)
	})

	bestOption := cont[0]

	// Has winner reached the quota?
	if currentStage[bestOption].Votes.GreaterThanOrEqual(result.Quota) {
		// If yes, he is elected.
		currentStage[bestOption] = optionResult{Votes: currentStage[bestOption].Votes, Status: elected}
		result.Elected = append(result.Elected, bestOption)
		result.Stages = append(result.Stages, currentStage)

		// Calc surplus, change weight for every vote which has the winner at
		// index zero, remove the winner of every vote.
		frac := currentStage[bestOption].Votes.Sub(result.Quota).Div(currentStage[bestOption].Votes)
		newValidVotes := make([]validVote, 0, len(validVotes))
		for _, vV := range validVotes {
			newWeight := vV.weight
			if vV.options[0] == bestOption {
				newWeight = newWeight.Mul(frac).Truncate(5)
			}
			var newOpts []int
			for _, o := range vV.options {
				if o != bestOption {
					newOpts = append(newOpts, o)
				}
			}
			if len(newOpts) != 0 {
				newValidVotes = append(newValidVotes, validVote{options: newOpts, weight: newWeight})
			}
		}

		// Prepare next stage and make recursive function call.
		nextStage := make(stage)
		for opt, optRes := range currentStage {
			if opt == bestOption {
				// This option was elected.
				nextStage[opt] = optionResult{Votes: result.Quota, Status: elected}
			} else if optRes.Status == elected || optRes.Status == excluded {
				// This option was elected or excluded in previous stages.
				nextStage[opt] = optRes
			} else {
				// This option is still continuing
				nextStage[opt] = optionResult{Votes: decimal.NewFromInt(0), Status: continuing}
			}
		}

		return resultHelper(result, vacancies-1, cont[1:], newValidVotes, nextStage)

	}
	// If not, the candidate with the then lowest number of votes is excluded.
	lastOption := cont[len(cont)-1]
	currentStage[lastOption] = optionResult{Votes: currentStage[lastOption].Votes, Status: excluded}
	result.Stages = append(result.Stages, currentStage)

	// Remove the looser of every vote.
	newValidVotes := make([]validVote, 0, len(validVotes))
	for _, vV := range validVotes {
		var newOpts []int
		for _, o := range vV.options {
			if o != lastOption {
				newOpts = append(newOpts, o)
			}
		}
		if len(newOpts) != 0 {
			newValidVotes = append(newValidVotes, validVote{options: newOpts, weight: vV.weight})
		}
	}

	// Prepare next stage and make recursive function call.
	nextStage := make(stage)
	for opt, optRes := range currentStage {
		if opt == lastOption {
			// This option was excluded.
			nextStage[opt] = optionResult{Votes: decimal.NewFromInt(0), Status: excluded}
		} else if optRes.Status == elected || optRes.Status == excluded {
			// This option was elected or excluded in previous stages.
			nextStage[opt] = optRes
		} else {
			// This option is still continuing
			nextStage[opt] = optionResult{Votes: decimal.NewFromInt(0), Status: continuing}
		}
	}

	return resultHelper(result, vacancies, cont[:len(cont)-1], newValidVotes, nextStage)
}

func validateConfig(config methodSTVScottishConfigWithOptions) error {
	if config.Posts > len(config.Options) {
		return fmt.Errorf("there are more (open) posts than options")
	}
	if hasDuplicates(config.Options) {
		return fmt.Errorf("config must not contain duplicates")
	}
	return nil
}
