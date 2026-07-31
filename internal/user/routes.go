package user

import "net/http"

// One public method, one tiny file: it's the feature's contract
// with the server. Handlers are kept private — from the outside, just this.

func (h Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/users", h.create)
	mux.HandleFunc("GET /v1/users", h.list)
	mux.HandleFunc("GET /v1/users/{id}", h.get)
	mux.HandleFunc("PATCH /v1/users/{id}", h.update)
	mux.HandleFunc("DELETE /v1/users/{id}", h.delete)
}