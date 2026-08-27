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
	DB  *sql.DB
}

func New(d Deps) http.Handler {
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

	// A per-request deadline. The server's WriteTimeout is a much wider net:
	// a handler stuck on a slow query keeps holding its goroutine and its
	// database connection until then.
	//
	// Caveat worth carrying to a gateway: TimeoutHandler buffers the response
	// and does not implement http.Flusher, so it is the wrong tool in front of
	// anything that streams. Here every response is a small JSON document.
	// It also writes the timeout body itself, so that one 503 is sniffed as
	// text/plain rather than problem+json.
	h = http.TimeoutHandler(h, d.Cfg.HandlerTimeout,
		`{"type":"about:blank","title":"Service Unavailable","status":503,`+
			`"detail":"the request took too long"}`)

	// Outermost first once composed: recoverPanic wraps the ResponseWriter
	// once, requestID runs before requestLogger so request_id shows up on the
	// log line, and the body limit is applied before any handler reads.
	h = requireJSON(h)
	h = maxBytes(d.Cfg.MaxBodyBytes)(h)
	h = secureHeaders(h)
	h = requestLogger(d.Log)(h)
	h = requestID(h)
	h = recoverPanic(d.Log)(h)

	return h

}
