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

// version はビルド時に -ldflags "-X main.version=..." で埋め込まれる。
// 未指定でビルドした場合は "dev" のままになる。
var version = "dev"

func main() {
	// `porthole check ...` はサーバを起動せず単発チェックを実行して終了コードを返す。
	// ECS Exec でシェルしか取れない環境や CI/CD から使うためのモード。
	if len(os.Args) > 1 && os.Args[1] == "check" {
		os.Exit(runCLI(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}

	port := flag.Int("port", envInt("PORT", 8080), "HTTP listen port")
	historySize := flag.Int("history", envInt("HISTORY_SIZE", 50), "Number of checks to keep in history")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	history := api.NewHistory(*historySize)
	apiHandler := api.New(history, version)

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
	log.Printf("Porthole %s listening on http://localhost%s", version, addr)
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
