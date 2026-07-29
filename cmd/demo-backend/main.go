// Command demo-backend поднимает простой upstream для Docker Compose и e2e demo.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func main() {
	address := environment("ADDRESS", ":8080")
	name := environment("NAME", "backend")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"backend": name, "method": r.Method, "path": r.URL.Path, "request_id": r.Header.Get("X-Request-ID")})
	})
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5_000_000_000}
	log.Printf("demo backend %s started at %s", name, address)
	log.Fatal(server.ListenAndServe())
}
func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
