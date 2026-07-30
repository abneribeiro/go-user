package server

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/abneribeiro/internal/logger"
	"github.com/abneribeiro/internal/problem"
)



func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b [8]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:])

		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(
			logger.WithRequestID(r.Context(), id)))
	})
}


type statusWriter struct {
	http.ResponseWriter
	status int
}


func (w *statusWriter) WriteHeader(code int)  {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}



func requestLogger(log *slog.Logger) func (http.Handler) http.Handler  {
	return  func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			log.InfoContext(r.Context(), "request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.status),
			slog.Duration("latency", time.Since(start)))
		})
	}	
}


func recoverPanic(log *slog.Logger) func(http.Handler) http.Handler  {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func ()  {
				if v := recover(); v != nil {
					log.ErrorContext(r.Context(), "panic",
						slog.Any("value", v),
						slog.String("stack", string(debug.Stack())))
						problem.Write(w, http.StatusInternalServerError, "internal server error")
					
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}



func maxBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}