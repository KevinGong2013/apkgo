package update

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	releaseAPI   = "https://api.github.com/repos/KevinGong2013/apkgo/releases/latest"
	stateFile    = "last_update_check"
	DefaultCheck = 30 * 24 * time.Hour // 30 days
)

type state struct {
	LastCheck string `json:"last_check"`
	Latest    string `json:"latest"`
}

func statePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".apkgo", stateFile)
}

// CheckAndRemind checks for updates if enough time has passed.
// Prints a reminder to stderr if a new version is available.
// This is non-blocking and best-effort — errors are silently ignored.
func CheckAndRemind(currentVersion string, interval time.Duration) {
	if interval <= 0 {
		return
	}

	path := statePath()

	// Read last check state
	var s state
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &s)
	}

	// Check if enough time has passed
	if s.LastCheck != "" {
		if t, err := time.Parse(time.RFC3339, s.LastCheck); err == nil {
			if time.Since(t) < interval {
				// Still within interval, but remind if we already know a newer version
				if s.Latest != "" && isNewer(currentVersion, s.Latest) {
					remind(s.Latest)
				}
				return
			}
		}
	}

	// Time to check — do it in background to not slow down the command
	go func() {
		latest, err := fetchLatest()
		if err != nil {
			slog.Debug("update check failed", "error", err)
			return
		}

		// Save state
		s.LastCheck = time.Now().UTC().Format(time.RFC3339)
		s.Latest = latest
		saveState(path, &s)

		if isNewer(currentVersion, latest) {
			remind(latest)
		}
	}()
}

func fetchLatest() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(releaseAPI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

func isNewer(current, latest string) bool {
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")
	return current != latest && current != "dev" && latest != ""
}

func remind(latest string) {
	fmt.Fprintf(os.Stderr, "\n  New version available: %s (current: run `apkgo upgrade` to update)\n\n", latest)
}

func saveState(path string, s *state) {
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.Marshal(s)
	os.WriteFile(path, data, 0644)
}
