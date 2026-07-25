package database

import (
	"context"
	"database/sql"
	"time"
)

func Open(ctx context.Context, dsn string) (*sql.DB, error)  {
	db, err := sql.Open("pgx", dsn) // n

	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxLifetime(time.Minute)

	ctx, cancel := context.WithTimeout(ctx, 5* time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return  nil, err
	}
	return db, nil
}