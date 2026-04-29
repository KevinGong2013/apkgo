package apkgo_test

import (
	"context"
	"testing"

	"github.com/KevinGong2013/apkgo/pkg/apkgo"
	"github.com/KevinGong2013/apkgo/pkg/config"
)

// TestDiagnose_NoStores verifies Diagnose surfaces a clear error
// when the filter eliminates every configured store. Cloud workers
// rely on this to distinguish "config wrong" from "all probes
// failed".
func TestDiagnose_NoStores(t *testing.T) {
	cfg := &config.Config{
		Stores: map[string]map[string]string{
			"pgyer": {"api_key": "bogus"},
		},
	}
	_, err := apkgo.Diagnose(context.Background(), apkgo.DiagnoseJob{
		Config: cfg,
		Stores: []string{"nonexistent"},
	})
	if err == nil {
		t.Fatal("expected error for empty filter result, got nil")
	}
}

// TestDiagnose_AnyFailed exercises the helper that cloud workers
// will call to make a yes/no scheduling decision.
func TestDiagnose_AnyFailed(t *testing.T) {
	r := &apkgo.DiagnoseResult{
		Stores: []apkgo.StoreReport{
			{Store: "good", Probes: nil},
		},
	}
	if r.AnyFailed() {
		t.Error("AnyFailed() = true on empty probes, want false")
	}
}

// TestDiagnose_RealProbe runs against a known-bogus pgyer api_key —
// the probe should fail cleanly (not panic, not crash the loop).
func TestDiagnose_RealProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("hits real network; -short to skip")
	}
	cfg := &config.Config{
		Stores: map[string]map[string]string{
			"pgyer": {"api_key": "definitely-not-a-real-key"},
		},
	}
	res, err := apkgo.Diagnose(context.Background(), apkgo.DiagnoseJob{
		Config: cfg,
		Stores: []string{"pgyer"},
	})
	if err != nil {
		t.Fatalf("Diagnose returned error: %v", err)
	}
	if len(res.Stores) != 1 {
		t.Fatalf("got %d store reports, want 1", len(res.Stores))
	}
	if !res.AnyFailed() {
		t.Error("expected AnyFailed=true with bogus api_key")
	}
}
