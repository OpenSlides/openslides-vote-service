package bulletin_board

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

func MessageVote(userID int, voteList [][]byte, controlHasheList [][]byte) (json.RawMessage, error) {
	data := struct {
		Type             string   `json:"type"`
		UserID           int      `json:"user_id"`
		VoteList         [][]byte `json:"vote_list"`
		ControlHasheList [][]byte `json:"contol_hash_list"`
	}{
		Type:             "vote",
		UserID:           userID,
		VoteList:         voteList,
		ControlHasheList: controlHasheList,
	}

	return json.Marshal(data)
}

func MessageStop() (json.RawMessage, error) {
	data := struct {
		Type string `json:"stop"`
	}{
		Type: "stop",
	}

	return json.Marshal(data)
}

func MessageMixed(userID int, mixedVotes []byte) (json.RawMessage, error) {
	data := struct {
		Type   string `json:"type"`
		UserID int    `json:"user_id"`
		Data   []byte `json:"data"`
	}{
		Type:   "mix",
		UserID: userID,
		Data:   mixedVotes,
	}

	return json.Marshal(data)
}

func MessagePublishResult(keys [][]byte, result []byte) (json.RawMessage, error) {
	data := struct {
		Type          string   `json:"type"`
		KeySecredList [][]byte `json:"key_secred_list"`
		Result        []byte   `json:"result"`
	}{
		Type:          "mix",
		KeySecredList: keys,
		Result:        result,
	}

	return json.Marshal(data)
}
