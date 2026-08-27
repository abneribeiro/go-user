package user

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeRepo struct {
	items  map[int64]User
	nextID int64
	err    error
}

func newFake() *fakeRepo {
	return &fakeRepo{items: make(map[int64]User), nextID: 1}
}

func (f *fakeRepo) Create(_ context.Context, t *User) error {
	if f.err != nil {
		return f.err
	}
	for _, existing := range f.items {
		if strings.EqualFold(existing.Email, t.Email) {
			return ErrDuplicate
		}
	}
	t.ID = f.nextID
	t.Version = 1
	f.nextID++
	f.items[t.ID] = *t
	return nil
}

func (f *fakeRepo) Get(_ context.Context, id int64) (User, error) {
	t, ok := f.items[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return t, nil
}

func (f *fakeRepo) List(context.Context, Filter) ([]User, int, error) {
	return nil, 0, nil
}

// Update mirrors the optimistic locking the real repository does. A fake that
// accepts any version would let the handler tests pass against behaviour the
// database does not have.
func (f *fakeRepo) Update(_ context.Context, t *User) error {
	cur, ok := f.items[t.ID]
	if !ok {
		return ErrNotFound
	}
	if cur.Version != t.Version {
		return ErrConflict
	}
	t.Version = cur.Version + 1
	f.items[t.ID] = *t
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, id int64) error {
	if _, ok := f.items[id]; !ok {
		return ErrNotFound
	}
	delete(f.items, id)
	return nil
}

// newTestServer returns a mux with the routes registered and the repo behind
// them, so tests can drive the handlers the way the server does.
func newTestServer(t *testing.T) (*http.ServeMux, *fakeRepo) {
	t.Helper()
	repo := newFake()
	h := NewHandler(repo, *slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, repo
}

func do(t *testing.T, mux *http.ServeMux, method, target, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func seed(t *testing.T, mux *http.ServeMux, name, email string) User {
	t.Helper()
	rec := do(t, mux, http.MethodPost, "/v1/users", `{"name":"`+name+`","email":"`+email+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed status = %d, wanted 201 (%s)", rec.Code, rec.Body)
	}
	var u User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatal(err)
	}
	return u
}

type validateCase struct {
	name    string
	in      CreateInput
	wantErr bool
}

func TestCreateInputValidate(t *testing.T) {
	cases := []validateCase{
		{name: "simple name", in: CreateInput{Name: "john doe", Email: "john@example.com"}},
		{name: "only spaces", in: CreateInput{Name: "   ", Email: "john@example.com"}, wantErr: true},
		{
			name:    "51 caracteres",
			in:      CreateInput{Name: strings.Repeat("a", 51), Email: "john@example.com"},
			wantErr: true,
		},
		{
			// mail.ParseAddress accepts this: it is a valid RFC 5322 address
			// with a display name, and storing it raw is identity spoofing.
			name:    "display name",
			in:      CreateInput{Name: "Abner", Email: "Victim <attacker@evil.com>"},
			wantErr: true,
		},
		{
			name:    "angle brackets only",
			in:      CreateInput{Name: "Abner", Email: "<a@b.com>"},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.in.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() = %v, queria erro: %v", err, c.wantErr)
			}
		})
	}
}

func TestCreateInputNormalizesEmail(t *testing.T) {
	in := CreateInput{Name: "Abner", Email: "  JOHN@Example.COM  "}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	// The unique index is on lower(email); without this the database is
	// case-insensitive and the application is not.
	if in.Email != "john@example.com" {
		t.Fatalf("email = %q, wanted %q", in.Email, "john@example.com")
	}
}

func TestCreateHandler(t *testing.T) {
	mux, _ := newTestServer(t)

	rec := do(t, mux, http.MethodPost, "/v1/users", `{"name":"john doe","email":"john@example.com"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, queria 201", rec.Code)
	}
	var got User
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID == 0 || got.Name != "john doe" || got.Email != "john@example.com" {
		t.Fatalf("resposta inesperada: %+v", got)
	}
	if want := "/v1/users/1"; rec.Header().Get("Location") != want {
		t.Fatalf("Location = %q, wanted %q", rec.Header().Get("Location"), want)
	}
	if want := `"1"`; rec.Header().Get("ETag") != want {
		t.Fatalf("ETag = %q, wanted %q", rec.Header().Get("ETag"), want)
	}
}

func TestProblemResponsesAreProblemJSON(t *testing.T) {
	mux, _ := newTestServer(t)

	rec := do(t, mux, http.MethodGet, "/v1/users/999999", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, wanted 404", rec.Code)
	}
	// A misspelled header name meant Go sniffed the body and clients checking
	// for problem+json never took that branch.
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, wanted application/problem+json", ct)
	}
	if rec.Header().Get("Context-Type") != "" {
		t.Fatal("the misspelled Context-Type header is back")
	}
	var p struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Type != "about:blank" {
		t.Fatalf("type = %q, wanted about:blank", p.Type)
	}
	if p.Status != http.StatusNotFound {
		t.Fatalf("status in body = %d, wanted 404", p.Status)
	}
	if strings.HasPrefix(p.Detail, " ") {
		t.Fatalf("detail = %q has a leading space", p.Detail)
	}
}

func TestCreateRejectsTrailingJSON(t *testing.T) {
	mux, repo := newTestServer(t)

	body := `{"name":"Trailing","email":"t1@x.com"}{"name":"IGNORED","email":"t2@x.com"}`
	rec := do(t, mux, http.MethodPost, "/v1/users", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, wanted 400", rec.Code)
	}
	if len(repo.items) != 0 {
		t.Fatalf("stored %d users, wanted none", len(repo.items))
	}
}

func TestDecodeErrorsMapToUsefulStatuses(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantDetail string
	}{
		{"empty body", "", http.StatusBadRequest, "body must not be empty"},
		{"unknown field", `{"name":"a","email":"a@b.com","is_admin":true}`, http.StatusBadRequest, "unknown field"},
		{"wrong type", `{"name":123,"email":"a@b.com"}`, http.StatusBadRequest, "wrong type"},
		{"syntax error", `{"name" "a"}`, http.StatusBadRequest, "malformed JSON at byte"},
		{"truncated body", `{"name":`, http.StatusBadRequest, "unexpected end of input"},
		{"trailing object", `{"name":"a","email":"a@b.com"}{}`, http.StatusBadRequest, "single JSON object"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux, _ := newTestServer(t)
			rec := do(t, mux, http.MethodPost, "/v1/users", c.body)
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, wanted %d (%s)", rec.Code, c.wantStatus, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), c.wantDetail) {
				t.Fatalf("body = %s, wanted it to mention %q", rec.Body, c.wantDetail)
			}
		})
	}
}

func TestCreateDisplayNameEmailIsRejected(t *testing.T) {
	mux, _ := newTestServer(t)

	rec := do(t, mux, http.MethodPost, "/v1/users",
		`{"name":"Abner","email":"Victim <attacker@evil.com>"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, wanted 422 (%s)", rec.Code, rec.Body)
	}
}

func TestCreateDuplicateEmail(t *testing.T) {
	mux, _ := newTestServer(t)
	seed(t, mux, "First", "dup@example.com")

	rec := do(t, mux, http.MethodPost, "/v1/users",
		`{"name":"Second","email":"DUP@EXAMPLE.COM"}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, wanted 409 (%s)", rec.Code, rec.Body)
	}
}

func TestGetInvalidID(t *testing.T) {
	mux, _ := newTestServer(t)

	for _, id := range []string{"abc", "0", "-1"} {
		rec := do(t, mux, http.MethodGet, "/v1/users/"+id, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("id %q: status = %d, wanted 400", id, rec.Code)
		}
	}
}

func TestDeleteIsNotSilentlyIdempotent(t *testing.T) {
	mux, _ := newTestServer(t)
	u := seed(t, mux, "Gone", "gone@example.com")

	if rec := do(t, mux, http.MethodDelete, "/v1/users/1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("first delete = %d, wanted 204", rec.Code)
	}
	// Deleting a row that is not there is not a success to report as one.
	if rec := do(t, mux, http.MethodDelete, "/v1/users/1", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d, wanted 404", rec.Code)
	}
	_ = u
}

func TestListRejectsOutOfRangePagination(t *testing.T) {
	mux, _ := newTestServer(t)

	// Without the cap, ?limit=10000000 is a one-line attack.
	for _, q := range []string{"?limit=0", "?limit=101", "?limit=10000000", "?limit=abc", "?offset=-1"} {
		rec := do(t, mux, http.MethodGet, "/v1/users"+q, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, wanted 400", q, rec.Code)
		}
	}
}

func TestUpdateRequiresIfMatch(t *testing.T) {
	mux, _ := newTestServer(t)
	seed(t, mux, "Abner", "abner@example.com")

	rec := do(t, mux, http.MethodPatch, "/v1/users/1", `{"name":"New"}`)

	// Without If-Match the PATCH is a blind read-modify-write, which is the
	// lost update this endpoint refuses to perform.
	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, wanted 428 (%s)", rec.Code, rec.Body)
	}
}

func TestUpdateRejectsStaleVersion(t *testing.T) {
	mux, _ := newTestServer(t)
	seed(t, mux, "Abner", "abner@example.com")

	// First writer wins and moves the version to 2.
	if rec := do(t, mux, http.MethodPatch, "/v1/users/1", `{"name":"Winner"}`,
		"If-Match", `"1"`); rec.Code != http.StatusOK {
		t.Fatalf("first patch = %d, wanted 200", rec.Code)
	}
	// Second writer still holds version 1 and must be told, not obeyed.
	rec := do(t, mux, http.MethodPatch, "/v1/users/1", `{"email":"other@example.com"}`,
		"If-Match", `"1"`)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale patch = %d, wanted 412 (%s)", rec.Code, rec.Body)
	}

	// And the winner's write survived intact.
	var got User
	getRec := do(t, mux, http.MethodGet, "/v1/users/1", "")
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Winner" || got.Email != "abner@example.com" {
		t.Fatalf("record got mixed: %+v", got)
	}
}

func TestUpdateAppliesOnlySentFields(t *testing.T) {
	mux, _ := newTestServer(t)
	seed(t, mux, "Abner", "abner@example.com")

	rec := do(t, mux, http.MethodPatch, "/v1/users/1", `{"name":"Abner Ribeiro"}`,
		"If-Match", `"1"`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, wanted 200 (%s)", rec.Code, rec.Body)
	}

	var got User
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Abner Ribeiro" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.Email != "abner@example.com" {
		t.Fatalf("email should not have changed, got %q", got.Email)
	}
	if got.Version != 2 {
		t.Fatalf("version = %d, wanted 2", got.Version)
	}
	if want := `"2"`; rec.Header().Get("ETag") != want {
		t.Fatalf("ETag = %q, wanted %q", rec.Header().Get("ETag"), want)
	}
}

func TestUpdateRejectsMalformedIfMatch(t *testing.T) {
	mux, _ := newTestServer(t)
	seed(t, mux, "Abner", "abner@example.com")

	for _, v := range []string{"*", "not-a-number", `"0"`, `""`} {
		rec := do(t, mux, http.MethodPatch, "/v1/users/1", `{"name":"New"}`, "If-Match", v)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("If-Match %q: status = %d, wanted 400", v, rec.Code)
		}
	}
}

func TestUpdateNotFound(t *testing.T) {
	mux, _ := newTestServer(t)

	rec := do(t, mux, http.MethodPatch, "/v1/users/999", `{"name":"New"}`, "If-Match", `"1"`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, wanted 404 (%s)", rec.Code, rec.Body)
	}
}

func TestUpdateValidatesFinalState(t *testing.T) {
	mux, _ := newTestServer(t)
	seed(t, mux, "Abner", "abner@example.com")

	rec := do(t, mux, http.MethodPatch, "/v1/users/1", `{"email":"Victim <attacker@evil.com>"}`,
		"If-Match", `"1"`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, wanted 422 (%s)", rec.Code, rec.Body)
	}
}
