// Package server is the HTTP adapter for the HTML frontend: it serves the
// page and the Snapshot JSON contract on loopback.
package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// Snapshots is what the server needs from the core.
type Snapshots interface {
	Current(ctx context.Context) (core.Snapshot, error)
}

// New returns the handler for `prledger serve`.
func New(src Snapshots, page []byte) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(page)
	})
	mux.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		snap, err := src.Current(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, snap)
	})
	return loopbackOnly(mux)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// loopbackOnly refuses requests whose Host is not a loopback name. The server
// only listens on loopback, but a page on another site could still reach it
// through a hostname that resolves to 127.0.0.1 (DNS rebinding); such
// requests carry that foreign hostname.
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "prledger only answers on localhost", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
