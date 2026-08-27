package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestWrapperKeepsFlusher is the one that matters for a reverse proxy: wrapping
// a ResponseWriter in a new type hides the optional interfaces the original
// implemented, and without Unwrap http.ResponseController cannot reach them
// either. SSE, streaming responses and a streaming ReverseProxy all die here.
func TestWrapperKeepsFlusher(t *testing.T) {
	var sawFlusher, sawHijacker, controllerWorks bool

	h := recoverPanic(discardLogger())(requestLogger(discardLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, sawFlusher = w.(http.Flusher)
			_, sawHijacker = w.(http.Hijacker)
			controllerWorks = http.NewResponseController(w).Flush() == nil
		})))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !sawFlusher {
		t.Error("handler does not see http.Flusher through the middleware chain")
	}
	if !sawHijacker {
		t.Error("handler does not see http.Hijacker: no WebSocket upgrade through this chain")
	}
	if !controllerWorks {
		t.Error("http.NewResponseController(w).Flush() failed: Unwrap is missing")
	}
}

// The chain must install exactly one wrapper, not one per middleware.
func TestWrapperIsInstalledOnce(t *testing.T) {
	var inner http.ResponseWriter

	h := recoverPanic(discardLogger())(requestLogger(discardLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inner = w
		})))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	rw, ok := inner.(*responseWriter)
	if !ok {
		t.Fatalf("handler got %T, wanted *responseWriter", inner)
	}
	if _, doubled := rw.ResponseWriter.(*responseWriter); doubled {
		t.Error("the ResponseWriter was wrapped twice")
	}
}

func TestRecoverPanicWritesProblemJSON(t *testing.T) {
	h := recoverPanic(discardLogger())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, wanted 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q", ct)
	}
}

// A panic after the response has started cannot be answered with a 500: the
// status is already on the wire, and writing again only earns a
// "superfluous response.WriteHeader call".
func TestRecoverPanicAfterHeaderDoesNotWriteAgain(t *testing.T) {
	h := recoverPanic(discardLogger())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"partial":true`))
			panic("boom")
		}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, wanted the 200 that was already sent", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "internal server error") {
		t.Fatal("a problem+json body was appended to an already-started response")
	}
}

// http.ErrAbortHandler is a deliberate panic — httputil.ReverseProxy uses it to
// abort a response. Swallowing it turns a normal event into a logged 500.
func TestRecoverPanicRepanicsErrAbortHandler(t *testing.T) {
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("recovered %v, wanted ErrAbortHandler to propagate", v)
		}
	}()

	h := recoverPanic(discardLogger())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			panic(http.ErrAbortHandler)
		}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	t.Fatal("ErrAbortHandler was swallowed")
}

func TestRequestIDHonoursSafeClientValue(t *testing.T) {
	var seen string
	h := requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = w.Header().Get("X-Request-ID")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "TRACE-FROM-CLIENT-123")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "TRACE-FROM-CLIENT-123" {
		t.Fatalf("request id = %q, wanted the client's trace to survive", seen)
	}
}

func TestRequestIDRejectsUnsafeClientValue(t *testing.T) {
	// The id ends up in log lines; newlines in it are log injection.
	unsafe := []string{
		"bad\r\nX-Injected: 1",
		"bad\nmore",
		strings.Repeat("a", 65),
		"has spaces",
		"semi;colon",
	}

	for _, v := range unsafe {
		var seen string
		h := requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = w.Header().Get("X-Request-ID")
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header["X-Request-Id"] = []string{v}
		h.ServeHTTP(httptest.NewRecorder(), req)

		if seen == v {
			t.Fatalf("unsafe id %q was accepted", v)
		}
		if len(seen) != 16 {
			t.Fatalf("id = %q, wanted a generated 16-char hex id", seen)
		}
	}
}

func TestRequireJSONRejectsOtherContentTypes(t *testing.T) {
	h := requireJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	cases := []struct {
		contentType string
		want        int
	}{
		{"application/json", http.StatusCreated},
		{"application/json; charset=utf-8", http.StatusCreated},
		// One of the three content types a plain <form> can send with no
		// CORS preflight — cheap defence against simple CSRF.
		{"text/plain", http.StatusUnsupportedMediaType},
		{"application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"multipart/form-data", http.StatusUnsupportedMediaType},
		{"", http.StatusUnsupportedMediaType},
	}

	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader("{}"))
		if c.contentType != "" {
			req.Header.Set("Content-Type", c.contentType)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != c.want {
			t.Errorf("Content-Type %q: status = %d, wanted %d", c.contentType, rec.Code, c.want)
		}
	}
}

func TestRequireJSONIgnoresReads(t *testing.T) {
	h := requireJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, m := range []string{http.MethodGet, http.MethodDelete, http.MethodHead} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/healthz", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, wanted 200", m, rec.Code)
		}
	}
}

// The 1MB limit was already enforced; the client just could not tell that
// size was the problem, because every decode error came back as 400.
func TestOversizedBodyIs413(t *testing.T) {
	const limit = 1024

	h := maxBytes(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var dst map[string]any
		if err := json.NewDecoder(r.Body).Decode(&dst); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	body := `{"name":"` + strings.Repeat("a", 2*limit) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, wanted 413", rec.Code)
	}
}

func TestSecureHeaders(t *testing.T) {
	h := secureHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, wanted %q", k, got, v)
		}
	}
}

func TestResponseWriterCountsBytes(t *testing.T) {
	payload := []byte(`{"ok":true}`)

	rw := &responseWriter{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	if _, err := rw.Write(payload); err != nil {
		t.Fatal(err)
	}

	if rw.bytes != int64(len(payload)) {
		t.Fatalf("bytes = %d, wanted %d", rw.bytes, len(payload))
	}
	if !rw.wroteHeader || rw.status != http.StatusOK {
		t.Fatalf("an implicit write should record a 200, got wrote=%v status=%d",
			rw.wroteHeader, rw.status)
	}
	if !bytes.Equal(rw.ResponseWriter.(*httptest.ResponseRecorder).Body.Bytes(), payload) {
		t.Fatal("the payload did not reach the underlying writer")
	}
}

// The per-request deadline has to actually fire, and the wrapper installed
// around it has to record the status it produced rather than the 200 it
// optimistically started with.
func TestTimeoutHandlerCutsSlowHandlers(t *testing.T) {
	var logged int

	stuck := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // a handler parked on a slow query
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var h http.Handler = http.TimeoutHandler(stuck, 10*time.Millisecond,
		`{"type":"about:blank","title":"Service Unavailable","status":503}`)
	h = requestLogger(logger)(h)
	h = recoverPanic(logger)(h)

	// Peek at what the chain recorded by wrapping once more on the outside.
	probe := &responseWriter{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	h.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, "/v1/users", nil))
	logged = probe.status

	if logged != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, wanted 503", logged)
	}
	if !strings.Contains(probe.ResponseWriter.(*httptest.ResponseRecorder).Body.String(), "about:blank") {
		t.Fatal("timeout body was not written")
	}
}
