// Command nethttp serves one route with the standard library's net/http: GET /plans/{id} answers a
// Medicare Advantage or ACA Marketplace plan's id and name, read from the haau3 platform API.
//
// One client serves the whole server, shared by every request. Each call takes the request's own
// context, so a client that goes away stops the call. Routers built on net/http, such as chi, work
// the same way.
//
//	HAAU3_API_KEY=... go run ./nethttp
//	curl localhost:8080/plans/H2406-013-000
package main

import (
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/haau3/haau3-go"
)

// handler is the server's routes, calling the API with client.
func handler(client *haau3.Client) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /plans/{id}", func(w http.ResponseWriter, r *http.Request) {
		plan, err := client.ReadInsurancePlan(r.Context(), r.PathValue("id"))
		var e *haau3.Error
		switch {
		case errors.As(err, &e) && e.Kind == haau3.KindRefused && e.Status == http.StatusNotFound:
			http.Error(w, "no such plan", http.StatusNotFound)
			return
		case err != nil:
			// The error logs safely: never the body the API answered with.
			slog.Error("reading a plan", "error", err)
			http.Error(w, "the plan could not be read", http.StatusBadGateway)
			return
		}
		name := ""
		if plan.Name != nil {
			name = *plan.Name
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": r.PathValue("id"), "name": name})
	})
	return mux
}

func main() {
	client, err := haau3.NewClient() // reads HAAU3_API_KEY
	if err != nil {
		log.Fatal(err)
	}
	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}
	log.Fatal(http.ListenAndServe(addr, handler(client)))
}
