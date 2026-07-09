package telemetry

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	endpoint = "https://apkgo.baici.tech/telemetry/v1/events"
	idFile   = ".apkgo_id"
)

// Event represents an anonymous usage event.
type Event struct {
	InstallID string        `json:"install_id"`
	Event     string        `json:"event"`  // "upload"
	Source    string        `json:"source"` // "cli"
	Version   string        `json:"version"`
	OS        string        `json:"os"`
	Arch      string        `json:"arch"`
	Package   string        `json:"package,omitempty"`
	Stores    []StoreResult `json:"stores,omitempty"`
	Timestamp int64         `json:"ts"`
}

// StoreResult is a per-store outcome (name + success only, no credentials or app data).
type StoreResult struct {
	Name    string `json:"name"`
	Success bool   `json:"ok"`
}

var (
	installID string
	once      sync.Once
)

func getInstallID() string {
	once.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			installID = "unknown"
			return
		}
		path := filepath.Join(home, idFile)
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			installID = string(data)
			return
		}
		installID = uuid.New().String()
		os.WriteFile(path, []byte(installID), 0644)
	})
	return installID
}

// Send fires an event asynchronously. Never blocks, never errors.
func Send(event Event) {
	event.InstallID = getInstallID()
	event.OS = runtime.GOOS
	event.Arch = runtime.GOARCH
	event.Timestamp = time.Now().Unix()

	go func() {
		body, err := json.Marshal(event)
		if err != nil {
			return
		}
		client := &http.Client{Timeout: 5 * time.Second}
		client.Post(endpoint, "application/json", bytes.NewReader(body))
	}()
}
