//go:build integration

package user_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/abneribeiro/internal/database"
	"github.com/abneribeiro/internal/user"
)

func TestRepositoryDuplicateName(t *testing.T) {
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

	// Different casing: the unique index is on lower(name).
	second := user.User{Name: "JOHN DOE", Email: "other@example.com"}
	err = repo.Create(ctx, &second)
	if !errors.Is(err, user.ErrDuplicate) {
		t.Fatalf("err = %v, wanted ErrDuplicate", err)
	}
}