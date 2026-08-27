package server

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/abneribeiro/internal/logger"
	"github.com/abneribeiro/internal/problem"
)

// responseWriter is the single wrapper in the chain. It is created once, by
// the outermost middleware, and everything inside reads from it.
type responseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

// Unwrap hands the original ResponseWriter to http.ResponseController, which
// is how Flush, Hijack and SetWriteDeadline reach through middleware since
// Go 1.20. Without it a wrapper silently strips http.Flusher from the handler,
// and SSE, streaming responses and a streaming ReverseProxy stop working.
// The rule for anything that wraps a ResponseWriter: always implement Unwrap.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader, w.status = true, code
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap alone is not enough: it fixes http.ResponseController, but code that
// type-asserts w.(http.Flusher) or w.(http.Hijacker) directly — which is most
// pre-1.20 code, and plenty of libraries still — sees a plain ResponseWriter.
// Forwarding both explicitly is what chi's WrapResponseWriter does too.

func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buf, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		// The connection belongs to the caller now: nothing downstream,
		// recoverPanic included, may write a status to it.
		w.wroteHeader = true
	}
	return conn, buf, err
}

// wrap returns the wrapper already in the chain, or installs one.
func wrap(w http.ResponseWriter) *responseWriter {
	if rw, ok := w.(*responseWriter); ok {
		return rw
	}
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

// safeRequestID guards what goes into the logs and back out in a header.
// An id that carries newlines is log injection; one with no length limit is
// an unbounded write on every log line.
func safeRequestID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Honour the caller's id so a trace survives across services, but
		// never trust its shape.
		id := r.Header.Get("X-Request-ID")
		if !safeRequestID(id) {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}

		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(
			logger.WithRequestID(r.Context(), id)))
	})
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := wrap(w)

			next.ServeHTTP(rw, r)

			log.InfoContext(r.Context(), "request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.status),
				slog.Int64("bytes", rw.bytes),
				slog.Duration("latency", time.Since(start)))
		})
	}
}

func recoverPanic(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := wrap(w)

			defer func() {
				v := recover()
				if v == nil {
					return
				}
				// ErrAbortHandler is a deliberate panic — it is how
				// httputil.ReverseProxy aborts a response. Logging it as an
				// error fills the logs with false positives.
				if v == http.ErrAbortHandler {
					panic(v)
				}

				log.ErrorContext(r.Context(), "panic",
					slog.Any("value", v),
					slog.String("stack", string(debug.Stack())))

				// The status is already on the wire: writing now only earns a
				// "superfluous response.WriteHeader call". Cutting the
				// connection is all that is left.
				if rw.wroteHeader {
					return
				}
				problem.Write(rw, http.StatusInternalServerError, "internal server error")
			}()

			next.ServeHTTP(rw, r)
		})
	}
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// requireJSON rejects bodies this API does not parse. text/plain is one of the
// three content types a plain <form> can send without a CORS preflight, so
// turning it away is cheap defence against simple CSRF as well as against
// ambiguity about how the body was meant to be read.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || ct != "application/json" {
				problem.Write(w, http.StatusUnsupportedMediaType,
					"expected Content-Type: application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func maxBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
