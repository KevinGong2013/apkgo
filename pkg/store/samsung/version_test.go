package samsung

import "testing"

// TestLatestBinary checks that the newest binary is chosen by highest
// versionCode (not list order), versionCode/binarySeq arrive as strings,
// and a missing binaryList yields zero values.
func TestLatestBinary(t *testing.T) {
	info := map[string]any{
		"binaryList": []any{
			map[string]any{"versionCode": "50", "versionName": "V3.7.50", "gms": "N", "binarySeq": "4"},
			map[string]any{"versionCode": "51", "versionName": "V3.8.51", "gms": "N", "binarySeq": "5"},
			map[string]any{"versionCode": "9", "versionName": "old", "gms": "Y", "binarySeq": "1"},
		},
	}
	if vn, vc, gms, seq := latestBinary(info); vc != 51 || vn != "V3.8.51" || gms != "N" || seq != "5" {
		t.Fatalf("got (%q, %d, %q, %q), want (V3.8.51, 51, N, 5)", vn, vc, gms, seq)
	}

	// binarySeq can arrive numeric; it must still come back as a string.
	numeric := map[string]any{
		"binaryList": []any{
			map[string]any{"versionCode": "7", "versionName": "v7", "gms": "N", "binarySeq": float64(2)},
		},
	}
	if _, _, _, seq := latestBinary(numeric); seq != "2" {
		t.Fatalf("numeric binarySeq: got %q, want 2", seq)
	}

	if vn, vc, gms, seq := latestBinary(map[string]any{}); vn != "" || vc != 0 || gms != "" || seq != "" {
		t.Fatalf("empty binaryList: got (%q, %d, %q, %q), want zero values", vn, vc, gms, seq)
	}
}
