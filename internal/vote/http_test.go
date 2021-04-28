package vote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type CreaterStub struct {
	id        int
	expectErr error
}

func (c *CreaterStub) Create(ctx context.Context, pollID int) error {
	c.id = pollID
	return c.expectErr
}

func TestHandleCreate(t *testing.T) {
	creater := &CreaterStub{}

	url := "/internal/vote/create"
	mux := http.NewServeMux()
	handleCreate(mux, creater)

	t.Run("Get request", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("GET", url, nil))

		if resp.Result().StatusCode != 405 {
			t.Errorf("Got status %s, expected 405 - Method not allowed", resp.Result().Status)
		}
	})

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

		if creater.id != 1 {
			t.Errorf("Creater was called with id %d, expected 1", creater.id)
		}
	})

	t.Run("Exist error", func(t *testing.T) {
		creater.expectErr = ErrExists

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

		if body.Error != "exist" {
			t.Errorf("Got error `%s`, expected `exist`", body.Error)
		}
	})

	t.Run("Internal error", func(t *testing.T) {
		creater.expectErr = errors.New("foobar")

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", strings.NewReader("request body")))

		if resp.Result().StatusCode != 500 {
			t.Errorf("Got status %s, expected 400", resp.Result().Status)
		}

		var body struct {
			Error string `json:"error"`
			MSG   string `json:"msg"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding resp body: %v", err)
		}

		if body.Error != "internal" {
			t.Errorf("Got error `%s`, expected `internal`", body.Error)
		}

		if body.MSG != "foobar" {
			t.Errorf("Got error message `%s`, expected `foobar`", body.MSG)
		}
	})
}

type StopperStub struct {
	id           int
	expectWriter string
	expectErr    error
}

func (s *StopperStub) Stop(ctx context.Context, pollID int, w io.Writer) error {
	s.id = pollID

	if s.expectErr != nil {
		return s.expectErr
	}
	_, err := w.Write([]byte(s.expectWriter))
	return err
}

func TestHandleStop(t *testing.T) {
	stopper := &StopperStub{}

	url := "/internal/vote/stop"
	mux := http.NewServeMux()
	handleStop(mux, stopper)

	t.Run("Get request", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("GET", url, nil))

		if resp.Result().StatusCode != 405 {
			t.Errorf("Got status %s, expected 405 - Method not allowed", resp.Result().Status)
		}
	})

	t.Run("No id", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url, nil))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400 - Bad Request", resp.Result().Status)
		}
	})

	t.Run("Valid", func(t *testing.T) {
		stopper.expectWriter = "some text"

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url+"?id=1", nil))

		if resp.Result().StatusCode != 200 {
			t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
		}

		if stopper.id != 1 {
			t.Errorf("Stopper was called with id %d, expected 1", stopper.id)
		}

		if resp.Body.String() != stopper.expectWriter {
			t.Errorf("Got body `%s`, expected `%s`", resp.Body.String(), stopper.expectWriter)
		}
	})

	t.Run("Not Exist error", func(t *testing.T) {
		stopper.expectErr = ErrNotExists

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

type ClearerStub struct {
	id        int
	expectErr error
}

func (c *ClearerStub) Clear(ctx context.Context, pollID int) error {
	c.id = pollID
	return c.expectErr
}

func TestHandleClear(t *testing.T) {
	clearer := &ClearerStub{}

	url := "/internal/vote/clear"
	mux := http.NewServeMux()
	handleClear(mux, clearer)

	t.Run("Get request", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("GET", url, nil))

		if resp.Result().StatusCode != 405 {
			t.Errorf("Got status %s, expected 405 - Method not allowed", resp.Result().Status)
		}
	})

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

		if clearer.id != 1 {
			t.Errorf("Clearer was called with id %d, expected 1", clearer.id)
		}
	})

	t.Run("Not Exist error", func(t *testing.T) {
		clearer.expectErr = ErrNotExists

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

type VoterStub struct {
	id        int
	user      int
	body      string
	expectErr error
}

func (v *VoterStub) Vote(ctx context.Context, pollID, requestUser int, r io.Reader) error {
	v.id = pollID
	v.user = requestUser

	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	v.body = string(body)
	return v.expectErr
}

type AuthError struct{}

func (AuthError) Error() string {
	return `{"error":"auth","msg":"auth error"}`
}

func (AuthError) Type() string {
	return "auth"
}

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

func TestHandleVote(t *testing.T) {
	voter := &VoterStub{}
	auther := &AutherStub{}

	url := "/system/vote"
	mux := http.NewServeMux()
	handleVote(mux, voter, auther)

	t.Run("Get request", func(t *testing.T) {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("GET", url, nil))

		if resp.Result().StatusCode != 405 {
			t.Errorf("Got status %s, expected 405 - Method not allowed", resp.Result().Status)
		}
	})

	t.Run("No id", func(t *testing.T) {
		auther.userID = 5

		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("POST", url, nil))

		if resp.Result().StatusCode != 400 {
			t.Errorf("Got status %s, expected 400 - Bad Request", resp.Result().Status)
		}
	})

	t.Run("ErrDoubleVote error", func(t *testing.T) {
		voter.expectErr = ErrDoubleVote

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

		if body.Error != "douple-vote" {
			t.Errorf("Got error `%s`, expected `douple-vote`", body.Error)
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
	mux := http.NewServeMux()
	handleHealth(mux)

	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, httptest.NewRequest("GET", url, nil))

	if resp.Result().StatusCode != 200 {
		t.Errorf("Got status %s, expected 200 - OK", resp.Result().Status)
	}

	expect := `{"health":true}`
	if got := resp.Body.String(); got != expect {
		t.Errorf("Got body `%s`, expected `%s`", got, expect)
	}
}
