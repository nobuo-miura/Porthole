package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/nobuo-miura/porthole/internal/api"
)

//go:embed web
var webFS embed.FS

func main() {
	port := flag.Int("port", envInt("PORT", 8080), "HTTP listen port")
	historySize := flag.Int("history", envInt("HISTORY_SIZE", 50), "Number of checks to keep in history")
	flag.Parse()

	history := api.NewHistory(*historySize)
	apiHandler := api.New(history)

	mux := http.NewServeMux()

	// API routes
	mux.Handle("/api/", apiHandler)
	mux.Handle("/healthz", apiHandler)

	// Serve embedded static files
	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("failed to create web sub-filesystem: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webRoot)))

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Porthole server listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
