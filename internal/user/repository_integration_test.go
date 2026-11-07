//go:build integration

package user_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/abneribeiro/go-user/internal/database"
	"github.com/abneribeiro/go-user/internal/user"
)

func TestRepositoryDuplicateEmail(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not defined")
	}
	ctx := context.Background()

	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// The migration code itself prepares the test database:
	// so the migration path is exercised on every run.
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users")
	})

	repo := user.NewPostgresRepository(db)

	first := user.User{Name: "John Doe", Email: "john@example.com"}
	if err := repo.Create(ctx, &first); err != nil {
		t.Fatal(err)
	}

	// Different name, same email in different casing: the unique index is
	// on lower(email), so only the email is expected to collide.
	second := user.User{Name: "Someone Else", Email: "JOHN@EXAMPLE.COM"}
	err = repo.Create(ctx, &second)
	if !errors.Is(err, user.ErrDuplicate) {
		t.Fatalf("err = %v, wanted ErrDuplicate", err)
	}
}

func TestRepositoryOptimisticLocking(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not defined")
	}
	ctx := context.Background()

	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM users")
	})

	repo := user.NewPostgresRepository(db)

	u := user.User{Name: "Abner", Email: "abner@example.com"}
	if err := repo.Create(ctx, &u); err != nil {
		t.Fatal(err)
	}
	if u.Version != 1 {
		t.Fatalf("version after create = %d, wanted 1", u.Version)
	}

	// Two readers hold the same version. The first write wins and bumps it.
	stale := u

	u.Name = "Abner Ribeiro"
	if err := repo.Update(ctx, &u); err != nil {
		t.Fatal(err)
	}
	if u.Version != 2 {
		t.Fatalf("version after update = %d, wanted 2", u.Version)
	}

	// The second write carries the stale version and must be rejected
	// instead of silently overwriting the first one.
	stale.Email = "other@example.com"
	if err := repo.Update(ctx, &stale); !errors.Is(err, user.ErrConflict) {
		t.Fatalf("err = %v, wanted ErrConflict", err)
	}

	// A stale version on a row that no longer exists is a 404, not a conflict.
	if err := repo.Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, &u); !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("err = %v, wanted ErrNotFound", err)
	}
}
