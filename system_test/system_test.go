package system_test

import (
	"net/http"
	"os"
	"testing"
)

const (
	addr = "http://localhost:9013"
)

func TestHealth(t *testing.T) {
	skip(t)

	req, err := http.NewRequest("GET", addr+"/system/vote/health", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Health request returned status %s", resp.Status)
	}
}

func skip(t *testing.T) {
	if _, ok := os.LookupEnv("VOTE_SYSTEM_TEST"); ok {
		t.SkipNow()
	}
}
