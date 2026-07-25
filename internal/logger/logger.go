package logger

import (
	"context"
	"log/slog"
	"os"
)

type ctxkey struct {}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxkey{}, id)
}

func RequestID(ctx context.Context) string  {
	id, _ := ctx.Value(ctxkey{}).(string)

	return  id
}


type handler struct {
	slog.Handler
}

func (h handler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}

	return h.Handler.Handle(ctx, r)
}


func New(env string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	var base slog.Handler = slog.NewTextHandler(os.Stdout, opts)
	if env == "production" {
		base = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler{Handler: base})
}