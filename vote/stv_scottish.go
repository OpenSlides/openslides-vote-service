package vote

import (
	"encoding/json"
	"fmt"

	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
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

func (m methodSTVScottish) ValidateVote(config string, vote json.RawMessage) error {
	fmt.Println(config, vote)
	return fmt.Errorf("Uh oh 1")
}

func (m methodSTVScottish) Result(config string, votes []dsmodels.Ballot) (string, error) {
	fmt.Println(config, votes)
	return "", fmt.Errorf("Uh oh 2")
}
