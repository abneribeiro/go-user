package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abneribeiro/internal/config"
	"github.com/abneribeiro/internal/database"
	"github.com/abneribeiro/internal/logger"
	"github.com/abneribeiro/internal/server"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main(){
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error  {
	cfg, err := config.Load()

	if err != nil{
		return err
	}

	lg := logger.New(cfg.Env)

	// root ctx: Ctrl+C or orchestrator's SIGTERM cancels everything
	// that descends from it — including in-flight queries.

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL)

	if err != nil {
		return err
	}

	defer db.Close()

	if err := database.Migrate(ctx, db); err != nil {
		return  err
	}

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler:  server.New(server.Deps{Cfg: cfg, Log:lg, DB: db}),
		ReadTimeout: cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout: time.Minute,
	}

	errCh := make(chan error, 1)

	go func() {
		lg.Info("listening", slog.String("addr", srv.Addr))

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		lg.Info("shutting down")
	}

	shutCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), cfg.ShutdownTimeout)

	defer cancel()


	return srv.Shutdown(shutCtx)
}