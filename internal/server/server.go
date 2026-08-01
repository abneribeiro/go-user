package server

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/abneribeiro/internal/config"
	"github.com/abneribeiro/internal/user"
)

type Deps struct {
	Cfg config.Config
	Log *slog.Logger
	DB *sql.DB
}

func New(d Deps) http.Handler  {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{"status": "ok"}`))
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := d.DB.PingContext(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return 
		}

		w.WriteHeader(http.StatusOK)
	})

	userRepo := user.NewPostgresRepository(d.DB)
	
	// 2. Instancia o Handler
	userHandler := user.NewHandler(userRepo, *d.Log)
	
	// 3. Registra as rotas de usuário no nosso mux!
	userHandler.RegisterRoutes(mux)



	var h http.Handler = mux

	h = maxBytes(d.Cfg.MaxBodyBytes)(h)
	h = requestLogger(d.Log)(h)
	h = requestID(h)
	h = recoverPanic(d.Log)(h)

	return h

}