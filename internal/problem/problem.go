package problem

import (
	"encoding/json"
	"net/http"
)

type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func Write(w http.ResponseWriter, status int, detail string) {
	// Serialize BEFORE writing the status, same reason as writeJSON:
	// a failure after WriteHeader leaves a truncated body behind a sent status.
	buf, err := json.Marshal(Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status), // "Not Found", "Conflict"…
		Status: status,
		Detail: detail,
	})
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}
