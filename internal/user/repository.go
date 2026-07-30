package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

type Repository interface {
	Create(ctx context.Context, u *User) error
	Get(ctx context.Context, id int64) (User, error)
	List(ctx context.Context, f Filter) ([]User, int, error)
	update(ctx context.Context, u *User) error
	delete(ctx context.Context, id int64) error
}

type Filter struct {
	Limit  int
	Offset int
}

type postgres struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) Repository {
	return postgres{db: db}
}

const columns = `id, name, email, created_at, updated_at`

func (r postgres) Create(ctx context.Context, u *User) error {

	const q = `INSERT INTO users (name, email) VALUES ($1, $2) RETURNING ` + columns

	err := r.db.QueryRowContext(ctx, q, u.Name, u.Email).Scan(&u.Name, &u.Email, &u.CreatedAt, &u.UpdatedAt)

	return translate(err)
}

func (r postgres) Get(ctx context.Context, id int64) (User, error) {
	const q = `SELECT ` + columns + `FROM users WHERE id = $1`

	var u User

	err := r.db.QueryRowContext(ctx, q, id).Scan(&u.Name, &u.Email, &u.CreatedAt, &u.UpdatedAt)

	return u, translate(err)
}

// The only function aware of Postgres error codes. If it didn't
// exist, every database error would become a 500 — including
// "duplicate title", which is the client's fault.

func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrDuplicate
		case "23514": // check_violation
			return fmt.Errorf("%w: %s", ErrInvalid, pgErr.ConstraintName)
		}
	}
	return err // inesperado: sobe cru, virará 500 com log completo
}
