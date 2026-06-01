package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/chaseputnam/theseus/internal/server"
)

func main() {
	port := flag.Int("port", 7000, "Port to listen on")
	dataDir := flag.String("data-dir", "data", "Data directory")
	staticDir := flag.String("static-dir", "static", "Static files directory")
	flag.Parse()

	// Env overrides
	if v := os.Getenv("PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*port = n
		}
	}
	if v := os.Getenv("DATA_DIR"); v != "" {
		*dataDir = v
	}
	if v := os.Getenv("STATIC_DIR"); v != "" {
		*staticDir = v
	}

	authEnabled := os.Getenv("AUTH_ENABLED") != "false"
	localhostBypass := os.Getenv("LOCALHOST_BYPASS") == "true"

	// Ensure data subdirectories exist
	for _, sub := range []string{"", "uploads", "generated_images", "personal_docs", "skills", "chroma"} {
		if err := os.MkdirAll(filepath.Join(*dataDir, sub), 0755); err != nil {
			log.Fatalf("failed to create data dir %s: %v", sub, err)
		}
	}

	srv, err := server.New(server.Config{
		Port:            *port,
		DataDir:         *dataDir,
		StaticDir:       *staticDir,
		AuthEnabled:     authEnabled,
		LocalhostBypass: localhostBypass,
	})
	if err != nil {
		log.Fatalf("server init: %v", err)
	}

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Theseus listening on http://localhost%s (data=%s, static=%s, auth=%v)",
		addr, *dataDir, *staticDir, authEnabled)

	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
