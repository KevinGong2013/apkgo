package samsung

import "testing"

// TestLatestBinary checks that the newest binary is chosen by highest
// versionCode (not list order), versionCode arrives as a string, and a
// missing binaryList yields zero values.
func TestLatestBinary(t *testing.T) {
	info := map[string]any{
		"binaryList": []any{
			map[string]any{"versionCode": "50", "versionName": "V3.7.50", "gms": "N"},
			map[string]any{"versionCode": "51", "versionName": "V3.8.51", "gms": "N"},
			map[string]any{"versionCode": "9", "versionName": "old", "gms": "Y"},
		},
	}
	if vn, vc, gms := latestBinary(info); vc != 51 || vn != "V3.8.51" || gms != "N" {
		t.Fatalf("got (%q, %d, %q), want (V3.8.51, 51, N)", vn, vc, gms)
	}

	if vn, vc, gms := latestBinary(map[string]any{}); vn != "" || vc != 0 || gms != "" {
		t.Fatalf("empty binaryList: got (%q, %d, %q), want zero values", vn, vc, gms)
	}
}
