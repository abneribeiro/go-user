package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/abneribeiro/internal/problem"
)

type Handler struct {
	repo Repository
	log  slog.Logger
}

func NewHandler(repo Repository, log slog.Logger) Handler {
	return Handler{repo: repo, log: log}
}

func (h Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput

	if err := decode(r, &in); err != nil {
		writeDecodeErr(w, err)
		return
	}

	if err := in.Validate(); err != nil {
		problem.Write(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	u := User{
		Name:  in.Name,
		Email: in.Email,
	}

	if err := h.repo.Create(r.Context(), &u); err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Location", "/v1/users/"+strconv.FormatInt(u.ID, 10))
	w.Header().Set("ETag", etag(u.Version))
	writeJSON(w, http.StatusCreated, u)
}

func (h Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		problem.Write(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}
	t, err := h.repo.Get(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", etag(t.Version))
	writeJSON(w, http.StatusOK, t)
}

func (h Handler) list(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		problem.Write(w, http.StatusBadRequest, err.Error())
		return
	}
	items, total, err := h.repo.List(r.Context(), f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page{
		Items:  items,
		Total:  total,
		Limit:  f.Limit,
		Offset: f.Offset,
	})
}

func (h Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		problem.Write(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}
	// The client has to say which revision it read. Without that, two
	// concurrent PATCHes each rewrite the whole row and both get a 200 —
	// the record ends up in a state neither request asked for.
	want, err := parseIfMatch(r)
	switch {
	case errors.Is(err, errNoIfMatch):
		problem.Write(w, http.StatusPreconditionRequired,
			"If-Match is required: send the ETag returned by GET /v1/users/{id}")
		return
	case err != nil:
		problem.Write(w, http.StatusBadRequest, err.Error())
		return
	}

	var in UpdateInput
	if err := decode(r, &in); err != nil {
		writeDecodeErr(w, err)
		return
	}

	// Reads, applies only the received fields, validates the entire result,
	// and saves. Validation checks the FINAL state, not just the submitted payload.
	t, err := h.repo.Get(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// Fail here for a better message; the UPDATE below re-checks the version
	// anyway, because anything can happen between this read and that write.
	if t.Version != want {
		h.fail(w, r, ErrConflict)
		return
	}
	if err := in.Apply(&t); err != nil {
		problem.Write(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// t.Version still carries the version we read: Update matches on it and
	// overwrites it with the new one.
	if err := h.repo.Update(r.Context(), &t); err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", etag(t.Version))
	writeJSON(w, http.StatusOK, t)
}

func (h Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		problem.Write(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent) // 204: no body, as it should be
}

var errSingleObject = errors.New("body must contain a single JSON object")

func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	// Misnamed fields are no longer silently ignored.
	// Saves hours of "why didn't my update do anything?".
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	// One body, one object. Anything after it is discarded silently otherwise,
	// and two intermediaries can then disagree about what the body contained.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errSingleObject
	}
	return nil
}

// writeDecodeErr turns a decode failure into the status that actually
// describes it. Flattening everything into 400 hides the 1MB body limit
// from the only party that can do something about it.
func writeDecodeErr(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	var typeErr *json.UnmarshalTypeError
	var synErr *json.SyntaxError

	switch {
	// First: a truncated body also surfaces as a SyntaxError.
	case errors.As(err, &maxErr):
		problem.Write(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("body must not exceed %d bytes", maxErr.Limit))
	case errors.As(err, &typeErr):
		problem.Write(w, http.StatusBadRequest,
			fmt.Sprintf("field %q has the wrong type", typeErr.Field))
	case errors.As(err, &synErr):
		problem.Write(w, http.StatusBadRequest,
			fmt.Sprintf("malformed JSON at byte %d", synErr.Offset))
	case errors.Is(err, io.EOF):
		problem.Write(w, http.StatusBadRequest, "body must not be empty")
	// A truncated document does not come back as a SyntaxError.
	case errors.Is(err, io.ErrUnexpectedEOF):
		problem.Write(w, http.StatusBadRequest, "malformed JSON body: unexpected end of input")
	case errors.Is(err, errSingleObject):
		problem.Write(w, http.StatusBadRequest, errSingleObject.Error())
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		problem.Write(w, http.StatusBadRequest,
			"unknown field "+strings.TrimPrefix(err.Error(), "json: unknown field "))
	default:
		problem.Write(w, http.StatusBadRequest, "malformed JSON body")
	}
}

var (
	errNoIfMatch  = errors.New("If-Match is required")
	errBadIfMatch = errors.New("If-Match must be an ETag like \"3\"")
)

// etag renders a version as a strong entity tag: the HTTP-native way of
// saying "this is the exact revision I read".
func etag(version int) string { return strconv.Quote(strconv.Itoa(version)) }

func parseIfMatch(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return 0, errNoIfMatch
	}
	// "*" is valid per RFC 9110 and means "any current representation", which
	// is exactly the blind overwrite this endpoint refuses to do.
	if raw == "*" {
		return 0, errBadIfMatch
	}
	v, err := strconv.Atoi(strings.Trim(strings.TrimPrefix(raw, "W/"), `"`))
	if err != nil || v < 1 {
		return 0, errBadIfMatch
	}
	return v, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	// Serialize BEFORE writing the status: if it fails after
	// WriteHeader, the 200 has already been sent and the body is truncated.
	buf, err := json.Marshal(v)
	if err != nil {
		problem.Write(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, ErrInvalid
	}
	return id, nil
}

// The ONLY bridge between domain errors and HTTP status codes. One function,
// one place: no handler decides this on its own.
func (h Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		problem.Write(w, http.StatusNotFound, "user not found")
	case errors.Is(err, ErrDuplicate):
		problem.Write(w, http.StatusConflict, "user already exists")
	case errors.Is(err, ErrConflict):
		problem.Write(w, http.StatusPreconditionFailed,
			"the user changed since you read it: GET it again and retry with the new ETag")
	case errors.Is(err, ErrConstraint):
		// Generic body, full detail in the log: the constraint name would
		// otherwise hand the client a piece of the schema.
		h.log.ErrorContext(r.Context(), "constraint violation", slog.Any("error", err))
		problem.Write(w, http.StatusUnprocessableEntity, ErrConstraint.Error())
	case errors.Is(err, ErrInvalid):
		problem.Write(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrInvalidEmail):
		problem.Write(w, http.StatusUnprocessableEntity, err.Error())
	default:
		// Raw error goes to the log with the request_id; the client
		// receives a generic message and nothing else.
		h.log.ErrorContext(r.Context(), "unhandled error",
			slog.Any("error", err))
		problem.Write(w, http.StatusInternalServerError, "internal server error")
	}
}

// Envelope instead of a top-level array: leaves room for the total today
// and for any metadata tomorrow, without breaking the client.
type page struct {
	Items  []User `json:"items"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

func parseFilter(r *http.Request) (Filter, error) {
	q := r.URL.Query()
	f := Filter{Limit: 20} // padrão são: nunca "tudo"

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		// The hard limit of 100 isn't for convenience, it's defense: without it,
		// ?limit=10000000 is a one-line attack.
		if err != nil || n < 1 || n > 100 {
			return f, errors.New("limit must be between 1 and 100")
		}
		f.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return f, errors.New("offset must be zero or greater")
		}
		f.Offset = n
	}
	return f, nil
}
