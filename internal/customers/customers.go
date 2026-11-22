// Package customers is the first domain we carved out of the legacy system.
// Same data, same URLs, but written the way we would write it today. Some of
// the differences are on purpose, so the shadow compare has something to
// find.
package customers

import (
	"encoding/json"
	"net/http"
)

type Customer struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Email  string   `json:"email"`
	City   string   `json:"city"`
	Phones []string `json:"phones"`
}

// Store is an in-memory store. In real life this is the new database that
// now owns the customers table.
type Store struct {
	list []Customer
}

func NewStore() *Store {
	return &Store{list: []Customer{
		{ID: "c-1", Name: "Jordi Vidal", Email: "jordi@example.com", City: "Barcelona", Phones: []string{"+34 600 000 001"}},
		// Empty list, not nil. Looks better, but the old clients get
		// [] where they always got null. Shadow mode will tell us.
		{ID: "c-2", Name: "Marta Puig", Email: "marta@example.com", City: "Girona", Phones: []string{}},
	}}
}

// Handler returns the HTTP API of the new customers service.
func Handler(s *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /customers", func(w http.ResponseWriter, r *http.Request) {
		list := []Customer{}
		for _, c := range s.list {
			if city := r.URL.Query().Get("city"); city == "" || city == c.City {
				list = append(list, c)
			}
		}
		write(w, http.StatusOK, map[string]any{"customers": list})
	})
	mux.HandleFunc("GET /customers/{id}", func(w http.ResponseWriter, r *http.Request) {
		for _, c := range s.list {
			if c.ID == r.PathValue("id") {
				write(w, http.StatusOK, c)
				return
			}
		}
		write(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	return mux
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
