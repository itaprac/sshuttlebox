package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	root := os.Getenv("SHBX_SITE_ROOT")
	if root == "" {
		root = "docs"
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/", staticHandler(absRoot))

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("serving %s on :%s", absRoot, port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func staticHandler(root string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(root))

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
			fullPath := filepath.Join(root, cleanPath)
			relPath, err := filepath.Rel(root, fullPath)
			if err != nil || relPath == ".." || strings.HasPrefix(relPath, "../") {
				http.NotFound(w, r)
				return
			}
			if info, err := os.Stat(fullPath); err != nil || info.IsDir() {
				serveIndex(w, r, root)
				return
			}
		}

		fileServer.ServeHTTP(w, r)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request, root string) {
	http.ServeFile(w, r, filepath.Join(root, "index.html"))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", strings.Join([]string{
			"default-src 'self'",
			"style-src 'self' 'unsafe-inline'",
			"script-src 'self' 'unsafe-inline'",
			"img-src 'self' data:",
			"base-uri 'none'",
			"form-action 'none'",
			"frame-ancestors 'none'",
		}, "; "))
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func init() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)
	log.SetPrefix(fmt.Sprintf("%s: ", filepath.Base(os.Args[0])))
}
