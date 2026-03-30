package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Event struct {
	InstallID  string        `json:"install_id"`
	Event      string        `json:"event"`
	Source     string        `json:"source"`
	Version   string        `json:"version"`
	OS        string        `json:"os"`
	Arch      string        `json:"arch"`
	Stores    []StoreResult `json:"stores,omitempty"`
	Timestamp int64         `json:"ts"`
	ReceivedAt string       `json:"received_at,omitempty"`
	RemoteAddr string       `json:"remote_addr,omitempty"`
}

type StoreResult struct {
	Name    string `json:"name"`
	Success bool   `json:"ok"`
}

// Stats aggregates metrics in memory, rebuilt from disk on startup.
type Stats struct {
	mu             sync.RWMutex
	TotalEvents    int64
	Installs       map[string]bool
	EventCounts    map[string]int64
	StoreCounts    map[string]int64
	StoreSuccess   map[string]int64
	VersionCounts  map[string]int64
	OSCounts       map[string]int64
	DailyCounts    map[string]int64 // "2026-03-30" → count
	LastEvent      string
}

func newStats() *Stats {
	return &Stats{
		Installs:      make(map[string]bool),
		EventCounts:   make(map[string]int64),
		StoreCounts:   make(map[string]int64),
		StoreSuccess:  make(map[string]int64),
		VersionCounts: make(map[string]int64),
		OSCounts:      make(map[string]int64),
		DailyCounts:   make(map[string]int64),
	}
}

func (s *Stats) record(e *Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.TotalEvents++
	s.LastEvent = e.ReceivedAt
	s.Installs[e.InstallID] = true
	s.EventCounts[e.Event]++

	if e.Version != "" {
		s.VersionCounts[e.Version]++
	}
	if e.OS != "" {
		s.OSCounts[e.OS+"/"+e.Arch]++
	}
	if len(e.ReceivedAt) >= 10 {
		s.DailyCounts[e.ReceivedAt[:10]]++
	}
	for _, st := range e.Stores {
		s.StoreCounts[st.Name]++
		if st.Success {
			s.StoreSuccess[st.Name]++
		}
	}
}

func (s *Stats) snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Last 30 days trend
	trend := make(map[string]int64)
	cutoff := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")
	for day, count := range s.DailyCounts {
		if day >= cutoff {
			trend[day] = count
		}
	}

	return map[string]any{
		"total_events":    s.TotalEvents,
		"unique_installs": len(s.Installs),
		"event_counts":    s.EventCounts,
		"store_counts":    s.StoreCounts,
		"store_success":   s.StoreSuccess,
		"version_counts":  s.VersionCounts,
		"os_counts":       s.OSCounts,
		"daily_trend":     trend,
		"last_event_at":   s.LastEvent,
	}
}

var (
	dataDir string
	stats   *Stats
	writeMu sync.Mutex
)

func main() {
	port := getEnv("PORT", "8080")
	dataDir = getEnv("DATA_DIR", "/data")
	os.MkdirAll(dataDir, 0755)

	stats = newStats()
	replayAll()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/events", handleEvent)
	mux.HandleFunc("GET /v1/stats", handleStats)
	mux.HandleFunc("GET /v1/events", handleListEvents)
	mux.HandleFunc("GET /healthz", handleHealth)

	slog.Info("telemetry server starting", "port", port, "data_dir", dataDir)
	if err := http.ListenAndServe(":"+port, cors(mux)); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func handleEvent(w http.ResponseWriter, r *http.Request) {
	var event Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}

	event.ReceivedAt = time.Now().UTC().Format(time.RFC3339)
	event.RemoteAddr = r.RemoteAddr

	line, _ := json.Marshal(event)
	line = append(line, '\n')

	filename := fmt.Sprintf("events_%s.jsonl", time.Now().UTC().Format("2006-01-02"))

	writeMu.Lock()
	err := appendFile(filepath.Join(dataDir, filename), line)
	writeMu.Unlock()

	if err != nil {
		slog.Error("write event", "error", err)
		writeJSON(w, 500, map[string]string{"error": "storage"})
		return
	}

	stats.record(&event)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, stats.snapshot())
}

func handleListEvents(w http.ResponseWriter, r *http.Request) {
	// Default: today. ?date=2026-03-30 for specific day
	day := r.URL.Query().Get("date")
	if day == "" {
		day = time.Now().UTC().Format("2006-01-02")
	}

	filename := filepath.Join(dataDir, fmt.Sprintf("events_%s.jsonl", day))
	data, err := os.ReadFile(filename)
	if err != nil {
		writeJSON(w, 200, map[string]any{"events": []any{}, "date": day})
		return
	}

	var events []json.RawMessage
	for _, line := range splitLines(data) {
		if len(line) > 0 {
			events = append(events, json.RawMessage(line))
		}
	}
	writeJSON(w, 200, map[string]any{"events": events, "date": day, "count": len(events)})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// --- helpers ---

func replayAll() {
	entries, _ := os.ReadDir(dataDir)
	// Sort to replay in chronological order
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dataDir, entry.Name()))
		if err != nil {
			continue
		}
		for _, line := range splitLines(data) {
			if len(line) == 0 {
				continue
			}
			var event Event
			if json.Unmarshal(line, &event) == nil {
				stats.record(&event)
			}
		}
	}
	slog.Info("replay complete", "events", stats.TotalEvents, "installs", len(stats.Installs))
}

func appendFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
