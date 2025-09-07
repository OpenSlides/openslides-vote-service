package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenSlides/openslides-vote-service/vote"
)

// TODO: Add tests for new handlers like create, update, reset...

type starterStub struct {
	id        int
	expectErr error
}

func (c *starterStub) Start(ctx context.Context, pollID int, requestUserID int) error {
	c.id = pollID
	return c.expectErr
}

func TestHandleStart(t *testing.T) {
	starter := &starterStub{}
	auth := &autherStub{userID: 1}

	url := "/vote/start"
	mux := testresolveError(handleStart(starter, auth))

	t.Run("No id", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url, nil))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400 - Bad Request", resp.Result().Status)
		}
	})

	t.Run("Invalid id", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=value", nil))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400 - Bad Request", resp.Result().Status)
		}
	})

	t.Run("Valid", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", strings.NewReader("request body")))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
		}

		if starter.id != 1 {
			t.Errorf("Start was called with id %d, expected 1", starter.id)
		}
	})

	t.Run("Error invalid", func(t *testing.T) {
		starter.expectErr = vote.ErrInvalid

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", strings.NewReader("request body")))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400", resp.Result().Status)
		}

		var body struct {
			Error string `json:"error"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding resp body: %v", err)
		}

		if body.Error != "invalid" {
			t.Errorf("Got error `%s`, expected `invalid`", body.Error)
		}
	})

	t.Run("Internal error", func(t *testing.T) {
		starter.expectErr = errors.New("test internal error")

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", strings.NewReader("request body")))

		if resp.Result().StatusCode != 500 {
			t.Errorf("Got status %s, expected 500", resp.Result().Status)
		}

		var body struct {
			Error string `json:"error"`
			MSG   string `json:"message"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding resp body: %v", err)
		}

		if body.Error != "internal" {
			t.Errorf("Got error `%s`, expected `internal`", body.Error)
		}

		if body.MSG != "Ups, something went wrong!" {
			t.Errorf("Got error message `%s`, expected `Ups, something went wrong!`", body.MSG)
		}
	})
}

type finalizerStub struct {
	id        int
	expectErr error

	expectedVotes   [][]byte
	expectedUserIDs []int
}

func (s *finalizerStub) Finalize(ctx context.Context, pollID int, requestUserID int, publish bool, anonymize bool) error {
	s.id = pollID

	if s.expectErr != nil {
		return s.expectErr
	}

	return nil
}

func TestHandleFinalize(t *testing.T) {
	finalizer := &finalizerStub{}
	auth := &autherStub{}

	url := "/vote/finalize"
	mux := handleFinalize(finalizer, auth)

	t.Run("No id", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url, nil))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400 - Bad Request", resp.Result().Status)
		}
	})

	t.Run("Valid", func(t *testing.T) {
		finalizer.expectedVotes = [][]byte{[]byte(`"some values"`)}

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
		}

		if finalizer.id != 1 {
			t.Errorf("Stopper was called with id %d, expected 1", finalizer.id)
		}

		expect := `{"votes":["some values"],"user_ids":[]}`
		if trimed := strings.TrimSpace(resp.Body.String()); trimed != expect {
			t.Errorf("Got body:\n`%s`, expected:\n`%s`", trimed, expect)
		}
	})

	t.Run("Not Exist error", func(t *testing.T) {
		finalizer.expectErr = vote.ErrNotExists

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400", resp.Result().Status)
		}

		var body struct {
			Error string `json:"error"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding resp body: %v", err)
		}

		if body.Error != "not-exist" {
			t.Errorf("Got error `%s`, expected `not-exist`", body.Error)
		}
	})
}

type voterStub struct {
	id        int
	user      int
	body      string
	expectErr error
}

func (v *voterStub) Vote(ctx context.Context, pollID, requestUserID int, r io.Reader) error {
	v.id = pollID
	v.user = requestUserID

	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	v.body = string(body)
	return v.expectErr
}

func TestHandleVote(t *testing.T) {
	voter := &voterStub{}
	auther := &autherStub{}

	url := "/system/vote"
	mux := handleVote(voter, auther)

	t.Run("No id", func(t *testing.T) {
		auther.userID = 5

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url, nil))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400 - Bad Request", resp.Result().Status)
		}
	})

	t.Run("ErrDoubleVote error", func(t *testing.T) {
		voter.expectErr = vote.ErrDoubleVote

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400", resp.Result().Status)
		}

		var body struct {
			Error string `json:"error"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding resp body: %v", err)
		}

		if body.Error != "double-vote" {
			t.Errorf("Got error `%s`, expected `double-vote`", body.Error)
		}
	})

	t.Run("Auth error", func(t *testing.T) {
		auther.authErr = true

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400", resp.Result().Status)
		}

		var body struct {
			Error string `json:"error"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding resp body: %v", err)
		}

		if body.Error != "auth" {
			t.Errorf("Got error `%s`, expected `auth`", body.Error)
		}
	})

	t.Run("Anonymous", func(t *testing.T) {
		auther.userID = 0
		auther.authErr = false

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

		if resp.Result().StatusCode != 401 {
			t.Errorf("Got status %s, expected 401", resp.Result().Status)
		}

		var body struct {
			Error string `json:"error"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding resp body: %v", err)
		}

		if body.Error != "not-allowed" {
			t.Errorf("Got error `%s`, expected `auth`", body.Error)
		}
	})

	t.Run("Valid", func(t *testing.T) {
		auther.userID = 5
		voter.body = "request body"
		voter.expectErr = nil

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", strings.NewReader("request body")))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
		}

		if voter.id != 1 {
			t.Errorf("Voter was called with id %d, expected 1", voter.id)
		}

		if voter.user != 5 {
			t.Errorf("Voter was called with userID %d, expected 5", voter.user)
		}

		if voter.body != "request body" {
			t.Errorf("Voter was called with body `%s` expected `request body`", voter.body)
		}
	})
}

func TestHandleHealth(t *testing.T) {
	url := "/system/vote/health"
	mux := handleHealth()

	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, httptest.NewRequest("GET", url, nil))

	if resp.Result().StatusCode != 200 {
		t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
	}

	expect := `{"healthy":true}`
	if got := resp.Body.String(); got != expect {
		t.Errorf("Got body `%s`, expected `%s`", got, expect)
	}
}

type AuthError struct{}

func (AuthError) Error() string {
	return `{"error":"auth","message":"auth error"}`
}

func (AuthError) Type() string {
	return "auth"
}

type autherStub struct {
	userID  int
	authErr bool
}

func (a *autherStub) Authenticate(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	if a.authErr {
		return nil, AuthError{}
	}
	return r.Context(), nil
}

func (a *autherStub) FromContext(context.Context) int {
	return a.userID
}

var testresolveError = getResolveError(func(fmt string, a ...any) (int, error) { return 0, nil })
