package bulletin_board

import (
	"encoding/json"

	"github.com/OpenSlides/openslides-go/datastore/dsmodels"
)

func MessageCreate(poll dsmodels.Poll) (json.RawMessage, error) {
	data := struct {
		Type string
		ID   int
	}{
		Type: "created",
		ID:   poll.ID,
	}

	return json.Marshal(data)
}

func MessagePublishKeyPublic(userID int, keyMixnet []byte, keyTrustee []byte) (json.RawMessage, error) {
	data := struct {
		Type             string
		UserID           int
		KeyPublicMixnet  []byte
		KeyPublicTrustee []byte
	}{
		Type:             "publish_public_key",
		UserID:           userID,
		KeyPublicMixnet:  keyMixnet,
		KeyPublicTrustee: keyTrustee,
	}

	return json.Marshal(data)
}

func MessageVote(userID int, vote []byte, controllHashes [][]byte) (json.RawMessage, error) {
	data := struct {
		Type           string
		UserID         int
		ControllHashes [][]byte
	}{
		Type:           "vote",
		UserID:         userID,
		ControllHashes: controllHashes,
	}

	return json.Marshal(data)
}

func MessageStop() (json.RawMessage, error) {
	data := struct {
		Type           string
		UserID         int
		ControllHashes [][]byte
	}{
		Type: "stop",
	}

	return json.Marshal(data)
}

func MessageMixed(userID int, mixedVotes []byte) (json.RawMessage, error) {
	data := struct {
		Type   string
		UserID int
		Data   []byte
	}{
		Type:   "mix",
		UserID: userID,
		Data:   mixedVotes,
	}

	return json.Marshal(data)
}

func MessagePublishResult(keys [][]byte, result []byte) (json.RawMessage, error) {
	data := struct {
		Type          string
		KeySecredList [][]byte
		Result        []byte
	}{
		Type:          "mix",
		KeySecredList: keys,
		Result:        result,
	}

	return json.Marshal(data)
}
