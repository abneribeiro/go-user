package user

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrNotFound     = errors.New("user not found")
	ErrInvalid      = errors.New("invalid user")
	ErrInvalidEmail = errors.New("invalid email")
	ErrDuplicate    = errors.New("user already exists")
	// ErrConflict means the row changed since the client read it: the
	// version carried by If-Match no longer matches what is stored.
	ErrConflict = errors.New("version conflict")
	// ErrConstraint is a database CHECK violation. Its message is deliberately
	// generic: the constraint name is a schema detail that belongs in the log,
	// not in a response body.
	ErrConstraint = errors.New("request violates a data constraint")
)

type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	// Version is the optimistic-locking token, surfaced to clients as an ETag.
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Input DTO separated from domain
// created_at, and cannot even try

type CreateInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (in *CreateInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = normalizeEmail(in.Email)

	return validate(in.Name, in.Email)
}

type UpdateInput struct {
	Name  *string `json:"name"`
	Email *string `json:"email"`
}

func (in *UpdateInput) Apply(t *User) error {
	if in.Name != nil {
		t.Name = strings.TrimSpace(*in.Name)
	}
	if in.Email != nil {
		t.Email = normalizeEmail(*in.Email)
	}
	return validate(t.Name, t.Email)
}

func validate(name, email string) error {
	switch {
	case name == "" || email == "":
		return fmt.Errorf("%w: name and email are required", ErrInvalid)
	// []rune instead of len(): len counts BYTES. "ação" has 4 letters
	// and 6 bytes, and the user counts letters.
	case len([]rune(name)) > 50:
		return fmt.Errorf("%w: name must be at most 50 characters", ErrInvalid)
	case len([]rune(email)) > 200:
		return fmt.Errorf("%w: email must be at most 200 characters", ErrInvalid)

	}

	// mail.ParseAddress implements RFC 5322, not "an email": it happily
	// accepts `Victim <attacker@evil.com>`. Discarding the parsed value and
	// storing the raw string is how a display name ends up in the database,
	// and from there in a To: header, a log line or a rendered page.
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return fmt.Errorf("%w: expected a plain address like name@example.com", ErrInvalidEmail)
	}

	return nil
}

// normalizeEmail lowercases as well as trims: the unique index is on
// lower(email), so without this the database is case-insensitive and the
// application is not.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
