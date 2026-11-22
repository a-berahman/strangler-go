// Package legacy is our pretend old system. It is small, but it has the
// habits I saw in almost every real one: null instead of an empty list,
// fields nobody remembers, totals as strings, and errors with status 200.
// Please do not fix them, the whole demo is about living with them.
package legacy

import (
	"encoding/json"
	"net/http"
)

type customer struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	City      string   `json:"city"`
	Phones    []string `json:"phones"` // nil slice, so it goes out as null
	LegacyRev int      `json:"legacy_rev"`
	Flags     string   `json:"_flags"`
}

type order struct {
	ID         string   `json:"id"`
	CustomerID string   `json:"customer_id"`
	Status     string   `json:"status"`
	Total      string   `json:"total"` // yes, a string
	Items      []string `json:"items"`
	LegacyRev  int      `json:"legacy_rev"`
}

var customers = []customer{
	{ID: "c-1", Name: "Jordi Vidal", Email: "jordi@example.com", City: "Barcelona", Phones: []string{"+34 600 000 001"}, LegacyRev: 7, Flags: "A1"},
	{ID: "c-2", Name: "Marta Puig", Email: "marta@example.com", City: "Girona", LegacyRev: 12, Flags: "A1"},
}

var orders = []order{
	{ID: "o-1001", CustomerID: "c-1", Status: "SHIPPED", Total: "42.50", Items: []string{"book", "lamp"}, LegacyRev: 3},
	{ID: "o-1002", CustomerID: "c-1", Status: "NEW", Total: "9.90", Items: []string{"cable"}, LegacyRev: 1},
}

// Handler returns the legacy HTTP API.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /customers", func(w http.ResponseWriter, r *http.Request) {
		var list []customer
		for _, c := range customers {
			if city := r.URL.Query().Get("city"); city == "" || city == c.City {
				list = append(list, c)
			}
		}
		write(w, http.StatusOK, map[string]any{"customers": list})
	})
	mux.HandleFunc("GET /customers/{id}", func(w http.ResponseWriter, r *http.Request) {
		for _, c := range customers {
			if c.ID == r.PathValue("id") {
				write(w, http.StatusOK, c)
				return
			}
		}
		// The old system never learned 404.
		write(w, http.StatusOK, map[string]string{"error": "not found"})
	})
	mux.HandleFunc("GET /orders", func(w http.ResponseWriter, r *http.Request) {
		var list []order
		for _, o := range orders {
			if cid := r.URL.Query().Get("customer"); cid == "" || cid == o.CustomerID {
				list = append(list, o)
			}
		}
		write(w, http.StatusOK, map[string]any{"orders": list})
	})
	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		// Nothing is stored, it is a demo. It is here to show that the
		// router does not shadow writes.
		write(w, http.StatusCreated, map[string]string{"id": "o-1003", "status": "NEW"})
	})
	return mux
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
