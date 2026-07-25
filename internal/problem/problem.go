package problem

import (
	"encoding/json"
	"net/http"
)

type Problem struct {
	Type string `json:"type"`
	Title string `json:"title"`
	Status int `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func Write(w http.ResponseWriter, status int, detail string)  {
	w.Header().Set("context-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem {
		Type: "about:blanck",
		Title: http.StatusText(status), //Not Found", "Conflict"…
		Status: status,
		Detail: detail,
	})
}

