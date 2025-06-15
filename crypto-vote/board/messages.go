package board

import (
	"encoding/json"

	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
)

// TODO: It would be nicer, if the type attribute would be outside the object.

func MessageCreate(poll dsmodels.Poll) (json.RawMessage, error) {
	data := struct {
		Type    string `json:"type"`
		ID      int    `json:"id"`
		MaxSize int    `json:"max_size"`
	}{
		Type:    "created",
		ID:      poll.ID,
		MaxSize: 1024,
	}

	return json.Marshal(data)
}

func MessagePublishKeyPublic(userID int, keyMixnet string, keyTrustee string) (json.RawMessage, error) {
	data := struct {
		Type             string `json:"type"`
		UserID           int    `json:"user_id"`
		KeyPublicMixnet  string `json:"key_public_mixnet"`
		KeyPublicTrustee string `json:"key_public_trustee"`
	}{
		Type:             "publish_public_key",
		UserID:           userID,
		KeyPublicMixnet:  keyMixnet,
		KeyPublicTrustee: keyTrustee,
	}

	return json.Marshal(data)
}

func MessageVote(userID int, voteList []string, controlData string) (json.RawMessage, error) {
	data := struct {
		Type        string   `json:"type"`
		UserID      int      `json:"user_id"`
		VoteList    []string `json:"vote_list"`
		ControlData string   `json:"contol_data"`
	}{
		Type:        "vote",
		UserID:      userID,
		VoteList:    voteList,
		ControlData: controlData,
	}

	return json.Marshal(data)
}

func MessageStop() (json.RawMessage, error) {
	data := struct {
		Type string `json:"type"`
	}{
		Type: "stop",
	}

	return json.Marshal(data)
}

func MessageMixed(userID int, mixedVotes string, amount int) (json.RawMessage, error) {
	data := struct {
		Type      string `json:"type"`
		UserID    int    `json:"user_id"`
		MixedData string `json:"mixed_data"`
		Amount    int    `json:"amount"`
	}{
		Type:      "mixed_data",
		UserID:    userID,
		MixedData: mixedVotes,
		Amount:    amount,
	}

	return json.Marshal(data)
}

func MessagePublishResult(keys []string, result json.RawMessage) (json.RawMessage, error) {
	data := struct {
		Type          string          `json:"type"`
		KeySecredList []string        `json:"key_secred_list"`
		Result        json.RawMessage `json:"result"`
	}{
		Type:          "mix",
		KeySecredList: keys,
		Result:        result,
	}

	return json.Marshal(data)
}
