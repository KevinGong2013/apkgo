package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Event struct {
	InstallID  string        `json:"install_id"`
	Event      string        `json:"event"`
	Source     string        `json:"source"`
	Version    string        `json:"version"`
	OS         string        `json:"os"`
	Arch       string        `json:"arch"`
	Package    string        `json:"package,omitempty"`
	Stores     []StoreResult `json:"stores,omitempty"`
	Timestamp  int64         `json:"ts"`
	ReceivedAt string        `json:"received_at,omitempty"`
	RemoteAddr string        `json:"remote_addr,omitempty"`
	ClientIP   string        `json:"client_ip,omitempty"`
}

type StoreResult struct {
	Name    string `json:"name"`
	Success bool   `json:"ok"`
}

// Stats aggregates metrics in memory, rebuilt from disk on startup.
type Stats struct {
	mu            sync.RWMutex
	TotalEvents   int64
	Installs      map[string]bool
	EventCounts   map[string]int64
	StoreCounts   map[string]int64
	StoreSuccess  map[string]int64
	VersionCounts map[string]int64
	OSCounts      map[string]int64
	PackageCounts map[string]int64
	DailyCounts   map[string]int64 // "2026-03-30" → count
	LastEvent     string
}

func newStats() *Stats {
	return &Stats{
		Installs:      make(map[string]bool),
		EventCounts:   make(map[string]int64),
		StoreCounts:   make(map[string]int64),
		StoreSuccess:  make(map[string]int64),
		VersionCounts: make(map[string]int64),
		OSCounts:      make(map[string]int64),
		PackageCounts: make(map[string]int64),
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
	if e.Package != "" {
		s.PackageCounts[e.Package]++
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
		"package_counts":  s.PackageCounts,
		"daily_trend":     trend,
		"last_event_at":   s.LastEvent,
	}
}

// tokenFile holds the generated admin token when ADMIN_TOKEN is unset.
const tokenFile = "admin_token"

var (
	dataDir    string
	stats      *Stats
	writeMu    sync.Mutex
	adminToken string
)

func main() {
	port := getEnv("PORT", "8080")
	dataDir = getEnv("DATA_DIR", "/data")
	os.MkdirAll(dataDir, 0755)

	var err error
	if adminToken, err = loadAdminToken(); err != nil {
		slog.Error("load admin token", "error", err)
		os.Exit(1)
	}

	stats = newStats()
	replayAll()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/events", handleEvent)
	mux.HandleFunc("GET /v1/stats", requireToken(handleStats))
	mux.HandleFunc("GET /v1/events", requireToken(handleListEvents))
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
	event.ClientIP = clientIP(r)

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
	// Reject anything that isn't a literal ISO date so user input can't
	// inject path separators or `..` segments into the filename below.
	// Reformatting the parsed time (rather than reusing `day`) ensures the
	// filename is built from a trusted value, not the raw query string.
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid date, expected YYYY-MM-DD"})
		return
	}
	day = parsed.Format("2006-01-02")

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

// loadAdminToken returns the bearer token guarding the read endpoints:
// ADMIN_TOKEN if set, otherwise one persisted in the data dir, generated
// on first start.
func loadAdminToken() (string, error) {
	if t := os.Getenv("ADMIN_TOKEN"); t != "" {
		return t, nil
	}
	path := filepath.Join(dataDir, tokenFile)
	if data, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(data)); t != "" {
			return t, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	t := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(t+"\n"), 0600); err != nil {
		return "", err
	}
	slog.Info("generated admin token", "path", path)
	return t, nil
}

func requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(adminToken)) != 1 {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// clientIP returns the caller's address. Behind the reverse proxy the peer
// is the proxy's private docker IP, so the real client is the last
// X-Forwarded-For hop — the one the proxy itself appended. The header is
// only trusted from private/loopback peers so direct hits can't spoof it.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !(peer.IsPrivate() || peer.IsLoopback()) {
		return host
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return host
	}
	hops := strings.Split(values[len(values)-1], ",")
	if last := strings.TrimSpace(hops[len(hops)-1]); last != "" {
		return last
	}
	return host
}

func replayAll() {
	entries, _ := os.ReadDir(dataDir)
	// Sort to replay in chronological order
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "events_") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dataDir, name))
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
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
