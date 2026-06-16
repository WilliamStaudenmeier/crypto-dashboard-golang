package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cacheEntry struct {
	payload   any
	expiresAt time.Time
}

type appState struct {
	client         *http.Client
	coingeckoBase  string
	coingeckoKey   string
	snapshotPath   string
	staticDir      string
	frontendOrigin string

	cacheMu sync.Mutex
	cache   map[string]cacheEntry

	snapshotMu sync.Mutex
}

func main() {
	state := &appState{
		client:         &http.Client{Timeout: 20 * time.Second},
		coingeckoBase:  strings.TrimRight(getEnv("COINGECKO_BASE_URL", "https://api.coingecko.com/api/v3"), "/"),
		coingeckoKey:   getEnv("COINGECKO_API_KEY", ""),
		snapshotPath:   getEnv("SNAPSHOT_PATH", "./db.json"),
		staticDir:      getEnv("STATIC_DIR", "./static"),
		frontendOrigin: getEnv("FRONTEND_ORIGIN", "*"),
		cache:          map[string]cacheEntry{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", state.handleHealth)
	mux.HandleFunc("/api/global", state.handleGlobal)
	mux.HandleFunc("/api/trending", state.handleTrending)
	mux.HandleFunc("/api/markets", state.handleMarkets)
	mux.HandleFunc("/api/history", state.handleHistory)
	mux.HandleFunc("/api/bootstrap", state.handleBootstrap)
	mux.Handle("/", state.handleStatic())

	handler := state.withCORS(mux)

	port := getEnv("PORT", "8080")
	addr := ":" + port
	log.Printf("Crypto Dashboard Golang listening on port %s", port)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (s *appState) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := s.frontendOrigin
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *appState) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *appState) writeNoCacheJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store")
	s.writeJSON(w, status, payload)
}

func (s *appState) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *appState) cacheGet(key string) (any, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.payload, true
}

func (s *appState) cacheSet(key string, payload any, ttl time.Duration) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cache[key] = cacheEntry{payload: payload, expiresAt: time.Now().Add(ttl)}
}

func (s *appState) fetchCoinGecko(path string, ttl time.Duration) (any, error) {
	if ttl > 0 {
		if cached, ok := s.cacheGet(path); ok {
			return cached, nil
		}
	}

	url := s.coingeckoBase + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if s.coingeckoKey != "" {
		req.Header.Set("x-cg-pro-api-key", s.coingeckoKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("coingecko request failed %d: %s", resp.StatusCode, string(body))
	}

	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if ttl > 0 {
		s.cacheSet(path, payload, ttl)
	}

	return payload, nil
}

func (s *appState) handleGlobal(w http.ResponseWriter, _ *http.Request) {
	payload, err := s.fetchCoinGecko("/global", 60*time.Second)
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Failed to fetch global market data"})
		return
	}
	s.writeJSON(w, http.StatusOK, payload)
}

func (s *appState) handleTrending(w http.ResponseWriter, _ *http.Request) {
	payload, err := s.fetchCoinGecko("/search/trending", 60*time.Second)
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Failed to fetch trending data"})
		return
	}
	s.writeJSON(w, http.StatusOK, payload)
}

func (s *appState) handleMarkets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	vsCurrency := strings.TrimSpace(q.Get("vs_currency"))
	if vsCurrency == "" {
		vsCurrency = "usd"
	}
	page := strings.TrimSpace(q.Get("page"))
	if page == "" {
		page = "1"
	}
	perPage := strings.TrimSpace(q.Get("per_page"))
	if perPage == "" {
		perPage = "20"
	}

	path := fmt.Sprintf("/coins/markets?vs_currency=%s&order=market_cap_desc&sparkline=false&price_change_percentage=24h&per_page=%s&page=%s", vsCurrency, perPage, page)
	payload, err := s.fetchCoinGecko(path, 30*time.Second)
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Failed to fetch market list"})
		return
	}
	s.writeJSON(w, http.StatusOK, payload)
}

func (s *appState) handleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	coinID := strings.TrimSpace(q.Get("coin_id"))
	if coinID == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "coin_id is required"})
		return
	}
	vsCurrency := strings.TrimSpace(q.Get("vs_currency"))
	if vsCurrency == "" {
		vsCurrency = "usd"
	}
	days := strings.TrimSpace(q.Get("days"))
	if days == "" {
		days = "365"
	}

	path := fmt.Sprintf("/coins/%s/market_chart?vs_currency=%s&days=%s&interval=daily", coinID, vsCurrency, days)
	payload, err := s.fetchCoinGecko(path, 300*time.Second)
	if err != nil {
		s.writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Failed to fetch market history"})
		return
	}
	s.writeJSON(w, http.StatusOK, payload)
}

func isCompleteBootstrap(payload map[string]any) bool {
	_, hasGlobal := payload["global"]
	_, hasTrending := payload["trending"]
	_, hasMarkets := payload["markets"]
	return hasGlobal && hasTrending && hasMarkets
}

func (s *appState) fetchLiveBootstrap() (map[string]any, error) {
	global, err := s.fetchCoinGecko("/global", 60*time.Second)
	if err != nil {
		return nil, err
	}
	trending, err := s.fetchCoinGecko("/search/trending", 60*time.Second)
	if err != nil {
		return nil, err
	}
	markets, err := s.fetchCoinGecko("/coins/markets?vs_currency=usd&order=market_cap_desc&sparkline=false&price_change_percentage=24h&per_page=20&page=1", 30*time.Second)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"global":   global,
		"trending": trending,
		"markets":  markets,
		"meta": map[string]any{
			"source":              "live",
			"updated_at_epoch_ms": time.Now().UnixMilli(),
		},
	}, nil
}

func (s *appState) readSnapshot() (map[string]any, bool) {
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	bytes, err := os.ReadFile(s.snapshotPath)
	if err != nil {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes, &payload); err != nil {
		return nil, false
	}
	if !isCompleteBootstrap(payload) {
		return nil, false
	}
	return payload, true
}

func (s *appState) writeSnapshot(payload map[string]any) {
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.snapshotPath), 0o755); err != nil {
		return
	}

	bytes, err := json.Marshal(payload)
	if err != nil {
		return
	}
	tmpPath := s.snapshotPath + ".tmp"
	if err := os.WriteFile(tmpPath, bytes, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, s.snapshotPath)
}

func asMap(value any) map[string]any {
	out, ok := value.(map[string]any)
	if !ok || out == nil {
		return map[string]any{}
	}
	return out
}

func (s *appState) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	forceRefresh := false
	if raw := strings.TrimSpace(r.URL.Query().Get("refresh")); raw != "" {
		parsed, _ := strconv.ParseBool(raw)
		forceRefresh = parsed || raw == "1"
	}

	if !forceRefresh {
		if snapshot, ok := s.readSnapshot(); ok {
			meta := asMap(snapshot["meta"])
			meta["source"] = "snapshot"
			meta["served_at_epoch_ms"] = time.Now().UnixMilli()
			snapshot["meta"] = meta
			s.writeNoCacheJSON(w, http.StatusOK, snapshot)
			return
		}
	}

	live, err := s.fetchLiveBootstrap()
	if err == nil {
		s.writeSnapshot(live)
		s.writeNoCacheJSON(w, http.StatusOK, live)
		return
	}

	if snapshot, ok := s.readSnapshot(); ok {
		meta := asMap(snapshot["meta"])
		meta["source"] = "snapshot-fallback"
		meta["served_at_epoch_ms"] = time.Now().UnixMilli()
		meta["warning"] = "live-refresh-failed"
		snapshot["meta"] = meta
		s.writeNoCacheJSON(w, http.StatusOK, snapshot)
		return
	}

	s.writeNoCacheJSON(w, http.StatusBadGateway, map[string]any{"error": "Failed to fetch bootstrap data"})
}

func (s *appState) handleStatic() http.Handler {
	fileServer := http.FileServer(http.Dir(s.staticDir))
	indexPath := filepath.Join(s.staticDir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
			http.NotFound(w, r)
			return
		}

		candidate := filepath.Join(s.staticDir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, indexPath)
	})
}
