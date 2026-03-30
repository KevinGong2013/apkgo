package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/KevinGong2013/apkgo/pkg/apk"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

// Record is a single upload history entry.
type Record struct {
	Timestamp string               `json:"timestamp"`
	APK       *apk.Info            `json:"apk"`
	Results   []*store.UploadResult `json:"results"`
}

// DefaultPath returns ~/.apkgo/history.jsonl
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".apkgo", "history.jsonl")
}

// Append adds a record to the history file (JSONL format, one JSON object per line).
func Append(path string, apkInfo *apk.Info, results []*store.UploadResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}

	record := Record{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		APK:       apkInfo,
		Results:   results,
	}

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(data)
	return err
}

// Read returns all records from the history file. Returns empty slice if file doesn't exist.
func Read(path string) ([]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var records []Record
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			continue // skip malformed lines
		}
		records = append(records, r)
	}
	return records, nil
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
