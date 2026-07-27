package uploader

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/KevinGong2013/apkgo/v3/pkg/apk"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

func TestNDJSONDoneIncludesRunMode(t *testing.T) {
	tests := []struct {
		name        string
		dryRun      bool
		sandbox     bool
		wantDryRun  bool
		wantSandbox bool
	}{
		{name: "production"},
		{name: "dry-run", dryRun: true, wantDryRun: true},
		{name: "sandbox", sandbox: true, wantSandbox: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			manager := NewNDJSONManager(&output)
			manager.SetRunMode(tt.dryRun, tt.sandbox)
			manager.Done(&apk.Info{}, []*store.UploadResult{{Store: "vivo", Success: true}})

			var event map[string]any
			if err := json.Unmarshal(output.Bytes(), &event); err != nil {
				t.Fatalf("decode event: %v", err)
			}
			if got, ok := event["dry_run"]; ok != tt.wantDryRun || (ok && got != true) {
				t.Errorf("dry_run = %v (present %v), want present %v", got, ok, tt.wantDryRun)
			}
			if got, ok := event["sandbox"]; ok != tt.wantSandbox || (ok && got != true) {
				t.Errorf("sandbox = %v (present %v), want present %v", got, ok, tt.wantSandbox)
			}
		})
	}
}
