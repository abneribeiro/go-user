package user

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrNotFound  = errors.New("user not found")
	ErrInvalid   = errors.New("invalid user")
	ErrInvalidEmail = errors.New("invalid email")
	ErrDuplicate = errors.New("user already exists")
)

type User struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Input DTO separated from domain
// created_at, and cannot even try

type CreateInput struct {
	Name      string    `json:"name"`
	Email     string    `json:"email"`
}



func (in *CreateInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(in.Email)

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
		t.Email = strings.TrimSpace(*in.Email)
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

	if _, err := mail.ParseAddress(email); err != nil {
		return fmt.Errorf("%w: invalid email", ErrInvalidEmail)
	}
	
	return nil
}