package user

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/abneribeiro/internal/problem"
)




type Handler struct {
	repo Repository
	log slog.Logger
}

func NewHandler(repo Repository, log slog.Logger) Handler  {
	return Handler{repo: repo, log: log}
}

func (h Handler) create(w http.ResponseWriter, r *http.Request)  {
	var in CreateInput

	if err := decode(r, &in); err != nil {
		problem.Write(w, http.StatusBadRequest, "malformed JSON body")
		return
	}

	if err := in.Validate(); err != nil {
		problem.Write(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	u := User{
		Name: in.Name,
		Email: in.Email,
	}

	if err := h.repo.Create(r.Context(), &u); err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Location", "/v1/users/"+strconv.FormatInt(u.ID, 10))
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
	var in UpdateInput
	if err := decode(r, &in); err != nil {
		problem.Write(w, http.StatusBadRequest, "malformed JSON body")
		return
	}

	// Reads, applies only the received fields, validates the entire result,
	// and saves. Validation checks the FINAL state, not just the submitted payload.
	t, err := h.repo.Get(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := in.Apply(&t); err != nil {
		problem.Write(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.repo.Update(r.Context(), &t); err != nil {
		h.fail(w, r, err)
		return
	}
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


func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	// Misnamed fields are no longer silently ignored.
	// Saves hours of "why didn't my update do anything?".
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
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
		problem.Write(w, http.StatusNotFound, " user not found")
	case errors.Is(err, ErrDuplicate):
		problem.Write(w, http.StatusConflict, "user already exists")
	case errors.Is(err, ErrInvalid):
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