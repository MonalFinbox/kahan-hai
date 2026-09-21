// Package api exposes the HTTP surface the frontend talks to.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/monalbarse/kahan-hai/backend/internal/adapter"
	"github.com/monalbarse/kahan-hai/backend/internal/store"
)

type Server struct {
	reg   *adapter.Registry
	store *store.Store
	log   *slog.Logger

	// defaultLoc is used when the client sends no coordinates, which is the
	// common case for a household tool pinned to one address.
	defaultLoc adapter.Location
}

func NewServer(reg *adapter.Registry, st *store.Store, log *slog.Logger, def adapter.Location) *Server {
	return &Server{reg: reg, store: st, log: log, defaultLoc: def}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	return cors(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"platforms": s.reg.Platforms(),
	})
}

type searchResponse struct {
	Query    string           `json:"query"`
	Location adapter.Location `json:"location"`
	TookMS   int64            `json:"tookMs"`
	Results  []adapter.Result `json:"results"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing q"})
		return
	}

	loc := s.defaultLoc
	if v := r.URL.Query().Get("lat"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			loc.Lat = f
		}
	}
	if v := r.URL.Query().Get("lon"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			loc.Lon = f
		}
	}

	start := time.Now()
	results := s.reg.SearchAll(r.Context(), query, loc)
	took := time.Since(start)

	for _, res := range results {
		if res.Err != "" {
			s.log.Warn("adapter failed", "platform", res.Platform, "err", res.Err)
		} else {
			s.log.Info("adapter ok", "platform", res.Platform, "n", len(res.Products), "ms", res.TookMS)
		}
	}

	// Persist outside the request's cancellation scope: a client that navigates
	// away should not discard data we already paid to collect.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.store.RecordSearch(ctx, query, loc, results); err != nil {
			s.log.Error("record search", "err", err)
		}
	}()

	writeJSON(w, http.StatusOK, searchResponse{
		Query: query, Location: loc, TookMS: took.Milliseconds(), Results: results,
	})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	platform, productID := q.Get("platform"), q.Get("productId")
	if platform == "" || productID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing platform or productId"})
		return
	}
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	points, err := s.store.History(r.Context(), platform, productID, limit)
	if err != nil {
		s.log.Error("history", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "history lookup failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"platform": platform, "productId": productID, "points": points})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
