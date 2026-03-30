package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// Event matches the CLI telemetry payload.
type Event struct {
	InstallID string        `json:"install_id"`
	Event     string        `json:"event"`
	Source    string        `json:"source"`
	Version  string        `json:"version"`
	OS       string        `json:"os"`
	Arch     string        `json:"arch"`
	Stores   []StoreResult `json:"stores,omitempty"`
	Timestamp int64        `json:"ts"`
	// Server-added fields
	ReceivedAt string `json:"received_at,omitempty"`
	RemoteAddr string `json:"remote_addr,omitempty"`
}

type StoreResult struct {
	Name    string `json:"name"`
	Success bool   `json:"ok"`
}

// Stats tracks aggregate metrics in memory.
type Stats struct {
	mu            sync.RWMutex
	TotalEvents   int64                    `json:"total_events"`
	UniqueInstalls map[string]bool          `json:"-"`
	InstallCount  int64                    `json:"unique_installs"`
	EventCounts   map[string]int64         `json:"event_counts"`
	StoreCounts   map[string]int64         `json:"store_counts"`
	StoreSuccess  map[string]int64         `json:"store_success"`
	VersionCounts map[string]int64         `json:"version_counts"`
	OSCounts      map[string]int64         `json:"os_counts"`
	LastEvent     string                   `json:"last_event_at"`
}

func NewStats() *Stats {
	return &Stats{
		UniqueInstalls: make(map[string]bool),
		EventCounts:    make(map[string]int64),
		StoreCounts:    make(map[string]int64),
		StoreSuccess:   make(map[string]int64),
		VersionCounts:  make(map[string]int64),
		OSCounts:       make(map[string]int64),
	}
}

func (s *Stats) Record(e *Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.TotalEvents++
	s.LastEvent = e.ReceivedAt

	if !s.UniqueInstalls[e.InstallID] {
		s.UniqueInstalls[e.InstallID] = true
		s.InstallCount++
	}

	s.EventCounts[e.Event]++
	if e.Version != "" {
		s.VersionCounts[e.Version]++
	}
	if e.OS != "" {
		s.OSCounts[e.OS+"/"+e.Arch]++
	}

	for _, store := range e.Stores {
		s.StoreCounts[store.Name]++
		if store.Success {
			s.StoreSuccess[store.Name]++
		}
	}
}

func (s *Stats) Snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]any{
		"total_events":    s.TotalEvents,
		"unique_installs": s.InstallCount,
		"event_counts":    s.EventCounts,
		"store_counts":    s.StoreCounts,
		"store_success":   s.StoreSuccess,
		"version_counts":  s.VersionCounts,
		"os_counts":       s.OSCounts,
		"last_event_at":   s.LastEvent,
	}
}

var (
	dataDir string
	stats   *Stats
)

func main() {
	port := getEnv("PORT", "8080")
	dataDir = getEnv("DATA_DIR", "/data")

	os.MkdirAll(dataDir, 0755)

	stats = NewStats()

	// Replay existing events to rebuild stats
	replayEvents()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/events", handleEvent)
	mux.HandleFunc("GET /v1/stats", handleStats)
	mux.HandleFunc("GET /v1/events", handleListEvents)
	mux.HandleFunc("GET /healthz", handleHealth)

	slog.Info("telemetry server starting", "port", port, "data_dir", dataDir)
	if err := http.ListenAndServe(":"+port, corsMiddleware(mux)); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func handleEvent(w http.ResponseWriter, r *http.Request) {
	var event Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	event.ReceivedAt = time.Now().UTC().Format(time.RFC3339)
	event.RemoteAddr = r.RemoteAddr

	// Append to daily log file (JSONL)
	filename := fmt.Sprintf("events_%s.jsonl", time.Now().UTC().Format("2006-01-02"))
	line, _ := json.Marshal(event)
	line = append(line, '\n')

	f, err := os.OpenFile(dataDir+"/"+filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("write event failed", "error", err)
		http.Error(w, `{"error":"storage"}`, http.StatusInternalServerError)
		return
	}
	f.Write(line)
	f.Close()

	// Update in-memory stats
	stats.Record(&event)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats.Snapshot())
}

func handleListEvents(w http.ResponseWriter, r *http.Request) {
	// Return today's events
	filename := fmt.Sprintf("events_%s.jsonl", time.Now().UTC().Format("2006-01-02"))
	data, err := os.ReadFile(dataDir + "/" + filename)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"events":[]}`))
		return
	}

	w.Header().Set("Content-Type", "application/jsonl")
	w.Write(data)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`{"status":"ok"}`))
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func replayEvents() {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(dataDir + "/" + entry.Name())
		if err != nil {
			continue
		}
		for _, line := range splitLines(data) {
			if len(line) == 0 {
				continue
			}
			var event Event
			if json.Unmarshal(line, &event) == nil {
				stats.Record(&event)
			}
		}
	}
	slog.Info("replayed events", "total", stats.TotalEvents, "installs", stats.InstallCount)
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
