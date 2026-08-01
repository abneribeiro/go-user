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
	t.ID = f.nextID
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

func (f *fakeRepo) Update(_ context.Context, t *User) error {
	if _, ok := f.items[t.ID]; !ok {
		return ErrNotFound
	}
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
			name:    "141 caracteres",
			in:      CreateInput{Name: strings.Repeat("a", 141), Email: "john@example.com"},
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

func TestCreateHandler(t *testing.T) {
	h := NewHandler(newFake(), *slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := strings.NewReader(`{"name":"john doe","email":"john@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/users", body)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

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
}