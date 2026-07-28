package apkgo

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinGong2013/apkgo/v3/pkg/config"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
	"github.com/KevinGong2013/apkgo/v3/pkg/uploader"
)

type sandboxTestStore struct {
	name  string
	calls *atomic.Int32
	fail  bool
}

func (s *sandboxTestStore) Name() string { return s.name }

func (s *sandboxTestStore) Upload(context.Context, *store.UploadRequest) *store.UploadResult {
	s.calls.Add(1)
	if s.fail {
		return store.ErrResult(s.name, time.Now(), errors.New("sandbox failed"))
	}
	return store.NewResult(s.name, time.Now())
}

func TestRunRejectsDryRunAndSandbox(t *testing.T) {
	_, err := Run(context.Background(), Job{
		Config:  &config.Config{},
		DryRun:  true,
		Sandbox: true,
	})
	if err == nil {
		t.Fatal("expected mutually exclusive mode error")
	}
}

func TestRunSandboxMixedModeSkipsSideEffects(t *testing.T) {
	var sandboxCalls atomic.Int32
	var productionCalls atomic.Int32
	var events atomic.Int32
	var gotEnvironment store.Environment

	store.RegisterWithEnvironment("test-run-sandbox", store.ConfigSchema{
		Name:            "test-run-sandbox",
		AcceptsAAB:      true,
		SupportsSandbox: true,
	}, func(_ map[string]string, environment store.Environment) (store.Store, error) {
		gotEnvironment = environment
		return &sandboxTestStore{name: "test-run-sandbox", calls: &sandboxCalls}, nil
	})
	store.Register("test-run-production", store.ConfigSchema{
		Name:       "test-run-production",
		AcceptsAAB: true,
	}, func(map[string]string) (store.Store, error) {
		return &sandboxTestStore{name: "test-run-production", calls: &productionCalls}, nil
	})

	dir := t.TempDir()
	aab := filepath.Join(dir, "app.aab")
	if err := os.WriteFile(aab, []byte("test bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	hookMarker := filepath.Join(dir, "hook-ran")
	hook := "touch " + hookMarker
	cfg := &config.Config{
		Hooks: config.HookConfig{Before: hook, After: hook},
		Stores: map[string]map[string]string{
			"test-run-sandbox":    {"before": hook, "after": hook},
			"test-run-production": {"before": hook, "after": hook},
		},
	}

	result, err := Run(context.Background(), Job{
		APKFile:  aab,
		Config:   cfg,
		Sandbox:  true,
		Progress: uploader.NopManager,
		Events: func(uploader.Event) {
			events.Add(1)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Sandbox || result.DryRun {
		t.Errorf("mode = sandbox %v, dry-run %v", result.Sandbox, result.DryRun)
	}
	if gotEnvironment != store.EnvironmentSandbox {
		t.Errorf("factory environment = %q", gotEnvironment)
	}
	if sandboxCalls.Load() != 1 || productionCalls.Load() != 0 {
		t.Errorf("calls = sandbox %d, production %d", sandboxCalls.Load(), productionCalls.Load())
	}
	if events.Load() != 0 {
		t.Errorf("sandbox emitted %d lifecycle events", events.Load())
	}
	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Errorf("sandbox hook ran, stat error = %v", err)
	}
	if cfg.Stores["test-run-sandbox"]["before"] != hook {
		t.Error("store creation mutated hook config")
	}

	results := make(map[string]*store.UploadResult, len(result.Results))
	for _, item := range result.Results {
		results[item.Store] = item
	}
	if item := results["test-run-sandbox"]; item == nil || !item.Success || !item.Sandbox || item.DryRun {
		t.Errorf("sandbox result = %+v", item)
	}
	if item := results["test-run-production"]; item == nil || !item.Success || item.Sandbox || !item.DryRun {
		t.Errorf("production-only result = %+v", item)
	}
}

func TestRunSandboxWithoutSupportedStoreIsDryRun(t *testing.T) {
	var calls atomic.Int32
	var logs bytes.Buffer
	store.Register("test-run-dry-only", store.ConfigSchema{
		Name:       "test-run-dry-only",
		AcceptsAAB: true,
	}, func(map[string]string) (store.Store, error) {
		return &sandboxTestStore{name: "test-run-dry-only", calls: &calls}, nil
	})

	aab := filepath.Join(t.TempDir(), "app.aab")
	if err := os.WriteFile(aab, []byte("test bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Job{
		APKFile: aab,
		Config: &config.Config{Stores: map[string]map[string]string{
			"test-run-dry-only": {},
		}},
		ReleaseTime: time.Now().Add(time.Hour),
		Sandbox:     true,
		Logger:      slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("production-only store called %d times", calls.Load())
	}
	if len(result.Results) != 1 || !result.Results[0].DryRun || result.Results[0].Sandbox {
		t.Errorf("result = %+v", result.Results)
	}
	if !strings.Contains(logs.String(), "scheduled release not supported") ||
		!strings.Contains(logs.String(), "test-run-dry-only") {
		t.Errorf("dry-run fallback skipped scheduled-release preflight, logs = %q", logs.String())
	}
}

func TestRunDryRunMarksPerStoreResult(t *testing.T) {
	var calls atomic.Int32
	store.Register("test-run-plain-dry", store.ConfigSchema{
		Name:       "test-run-plain-dry",
		AcceptsAAB: true,
	}, func(map[string]string) (store.Store, error) {
		return &sandboxTestStore{name: "test-run-plain-dry", calls: &calls}, nil
	})

	aab := filepath.Join(t.TempDir(), "app.aab")
	if err := os.WriteFile(aab, []byte("test bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Job{
		APKFile: aab,
		Config: &config.Config{Stores: map[string]map[string]string{
			"test-run-plain-dry": {},
		}},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.DryRun || result.Sandbox || calls.Load() != 0 {
		t.Errorf("result mode = %+v, calls = %d", result, calls.Load())
	}
	if len(result.Results) != 1 || !result.Results[0].DryRun {
		t.Errorf("result = %+v", result.Results)
	}
}

func TestSandboxResultsMarksFailedUpload(t *testing.T) {
	failed := store.ErrResult("vivo", time.Now(), errors.New("upload failed"))
	results := sandboxResults([]string{"vivo", "huawei"}, []bool{true, false}, []*store.UploadResult{failed})

	if results[0].Success || !results[0].Sandbox || results[0].DryRun {
		t.Errorf("failed sandbox result = %+v", results[0])
	}
	if !results[1].Success || results[1].Sandbox || !results[1].DryRun {
		t.Errorf("dry-run fallback result = %+v", results[1])
	}
}

func TestRunSandboxUsesConfiguredStoreTypeForCapability(t *testing.T) {
	var calls atomic.Int32
	store.RegisterWithEnvironment("test-run-capability-name", store.ConfigSchema{
		Name:            "test-run-capability-name",
		AcceptsAAB:      true,
		SupportsSandbox: true,
	}, func(_ map[string]string, _ store.Environment) (store.Store, error) {
		return &sandboxTestStore{name: "test-run-capability-name", calls: &calls}, nil
	})
	store.Register("test-run-name-collision", store.ConfigSchema{
		Name:       "test-run-name-collision",
		AcceptsAAB: true,
	}, func(map[string]string) (store.Store, error) {
		// A runtime display name must not grant another registered type's
		// sandbox capability.
		return &sandboxTestStore{name: "test-run-capability-name", calls: &calls}, nil
	})

	aab := filepath.Join(t.TempDir(), "app.aab")
	if err := os.WriteFile(aab, []byte("test bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Job{
		APKFile: aab,
		Config: &config.Config{Stores: map[string]map[string]string{
			"test-run-name-collision": {},
		}},
		Sandbox: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("production-only store with colliding name called %d times", calls.Load())
	}
	if len(result.Results) != 1 || !result.Results[0].DryRun || result.Results[0].Sandbox {
		t.Errorf("result = %+v", result.Results)
	}
}
