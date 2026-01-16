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

type createrStub struct {
	requestUserID int
	body          string
	pollID        int
	expectErr     error
}

func (c *createrStub) Create(ctx context.Context, requestUserID int, r io.Reader) (int, error) {
	c.requestUserID = requestUserID

	body, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	c.body = string(body)

	return c.pollID, c.expectErr
}

func TestHandleCreate(t *testing.T) {
	creater := &createrStub{pollID: 42}
	auth := &AutherStub{userID: 1}

	url := "/system/vote/create"
	mux := testresolveError(handleCreate(creater, auth))

	t.Run("Valid", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url, strings.NewReader("create data")))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
		}

		if creater.requestUserID != 1 {
			t.Errorf("Create was called with userID %d, expected 1", creater.requestUserID)
		}

		if creater.body != "create data" {
			t.Errorf("Create was called with body `%s`, expected `create data`", creater.body)
		}

		var body struct {
			PollID int `json:"poll_id"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding resp body: %v", err)
		}

		if body.PollID != 42 {
			t.Errorf("Got poll_id %d, expected 42", body.PollID)
		}
	})

	t.Run("Error invalid", func(t *testing.T) {
		creater.expectErr = vote.ErrInvalid

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url, strings.NewReader("invalid data")))

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
		creater.expectErr = errors.New("test internal error")

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url, strings.NewReader("data")))

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

type updaterStub struct {
	pollID        int
	requestUserID int
	body          string
	expectErr     error
}

func (u *updaterStub) Update(ctx context.Context, pollID int, requestUserID int, r io.Reader) error {
	u.pollID = pollID
	u.requestUserID = requestUserID

	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	u.body = string(body)

	return u.expectErr
}

func TestHandleUpdate(t *testing.T) {
	updater := &updaterStub{}
	auth := &AutherStub{userID: 1}

	url := "/system/vote/update"
	mux := testresolveError(handleUpdate(updater, auth))

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
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", strings.NewReader("update data")))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
		}

		if updater.pollID != 1 {
			t.Errorf("Update was called with pollID %d, expected 1", updater.pollID)
		}

		if updater.requestUserID != 1 {
			t.Errorf("Update was called with userID %d, expected 1", updater.requestUserID)
		}

		if updater.body != "update data" {
			t.Errorf("Update was called with body `%s`, expected `update data`", updater.body)
		}
	})

	t.Run("Error not exist", func(t *testing.T) {
		updater.expectErr = vote.ErrNotExists

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", strings.NewReader("data")))

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

	t.Run("Internal error", func(t *testing.T) {
		updater.expectErr = errors.New("test internal error")

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", strings.NewReader("data")))

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

type deleterStub struct {
	pollID        int
	requestUserID int
	expectErr     error
}

func (d *deleterStub) Delete(ctx context.Context, pollID int, requestUserID int) error {
	d.pollID = pollID
	d.requestUserID = requestUserID
	return d.expectErr
}

func TestHandleDelete(t *testing.T) {
	deleter := &deleterStub{}
	auth := &AutherStub{userID: 1}

	url := "/system/vote/delete"
	mux := testresolveError(handleDelete(deleter, auth))

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
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
		}

		if deleter.pollID != 1 {
			t.Errorf("Delete was called with pollID %d, expected 1", deleter.pollID)
		}

		if deleter.requestUserID != 1 {
			t.Errorf("Delete was called with userID %d, expected 1", deleter.requestUserID)
		}
	})

	t.Run("Error not exist", func(t *testing.T) {
		deleter.expectErr = vote.ErrNotExists

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

	t.Run("Internal error", func(t *testing.T) {
		deleter.expectErr = errors.New("test internal error")

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

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
	auth := &AutherStub{userID: 1}

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
	publish   bool
	anonymize bool
	expectErr error
}

func (s *finalizerStub) Finalize(ctx context.Context, pollID int, requestUserID int, publish bool, anonymize bool) error {
	s.id = pollID
	s.publish = publish
	s.anonymize = anonymize

	if s.expectErr != nil {
		return s.expectErr
	}

	return nil
}

func TestHandleFinalize(t *testing.T) {
	finalizer := &finalizerStub{}
	auth := &AutherStub{userID: 1}

	url := "/vote/finalize"
	mux := testresolveError(handleFinalize(finalizer, auth))

	t.Run("No id", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url, nil))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400 - Bad Request", resp.Result().Status)
		}
	})

	t.Run("Valid", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
		}

		if finalizer.id != 1 {
			t.Errorf("Finanlizer was called with id %d, expected 1", finalizer.id)
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

	t.Run("Publish", func(t *testing.T) {
		finalizer.expectErr = nil

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1&publish", nil))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK.\nBody: %s", resp.Result().Status, resp.Body.String())
		}

		if !finalizer.publish {
			t.Errorf("Finanlizer was not called with publish")
		}
	})

	t.Run("Anonymize", func(t *testing.T) {
		finalizer.expectErr = nil

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1&anonymize", nil))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK.\nBody: %s", resp.Result().Status, resp.Body.String())
		}

		if !finalizer.anonymize {
			t.Errorf("Finanlizer was not called with anonymize")
		}
	})
}

type reseterStub struct {
	pollID        int
	requestUserID int
	expectErr     error
}

func (r *reseterStub) Reset(ctx context.Context, pollID int, requestUserID int) error {
	r.pollID = pollID
	r.requestUserID = requestUserID
	return r.expectErr
}

func TestHandleReset(t *testing.T) {
	reseter := &reseterStub{}
	auth := &AutherStub{userID: 1}

	url := "/system/vote/reset"
	mux := testresolveError(handleReset(reseter, auth))

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
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
		}

		if reseter.pollID != 1 {
			t.Errorf("Reset was called with pollID %d, expected 1", reseter.pollID)
		}

		if reseter.requestUserID != 1 {
			t.Errorf("Reset was called with userID %d, expected 1", reseter.requestUserID)
		}
	})

	t.Run("Error not exist", func(t *testing.T) {
		reseter.expectErr = vote.ErrNotExists

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

	t.Run("Internal error", func(t *testing.T) {
		reseter.expectErr = errors.New("test internal error")

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

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
	auther := &AutherStub{}

	url := "/system/vote"
	mux := testresolveError(handleVote(voter, auther))

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

	expect := `{"healthy": true, "service":"vote"}`
	if got := resp.Body.String(); got != expect {
		t.Errorf("Got body `%s`, expected `%s`", got, expect)
	}
}

type AuthError struct{}

func (AuthError) Error() string {
	return `auth error`
}

func (AuthError) Type() string {
	return "auth"
}

// AutherSub fakes auth
type AutherStub struct {
	userID  int
	authErr bool
}

func (a *AutherStub) Authenticate(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	if a.authErr {
		return nil, AuthError{}
	}
	return r.Context(), nil
}

func (a *AutherStub) FromContext(context.Context) int {
	return a.userID
}

var testresolveError = getResolveError(func(fmt string, a ...any) (int, error) { return 0, nil })
