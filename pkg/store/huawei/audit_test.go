package huawei

import (
	"testing"

	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

// TestMapHuaweiReleaseState locks in the releaseState → unified-state
// mapping (the audit query's only non-trivial logic, and untestable
// end-to-end without real credentials).
func TestMapHuaweiReleaseState(t *testing.T) {
	cases := map[int]store.AuditState{
		4: store.AuditReviewing, 5: store.AuditReviewing, 12: store.AuditReviewing,
		0: store.AuditApproved, 3: store.AuditApproved,
		1: store.AuditRejected, 8: store.AuditRejected, 13: store.AuditRejected,
		2: store.AuditWithdrawn, 10: store.AuditWithdrawn, 11: store.AuditWithdrawn,
		7: store.AuditUnknown, 99: store.AuditUnknown,
	}
	for state, want := range cases {
		if got, _ := mapHuaweiReleaseState(state); got != want {
			t.Errorf("mapHuaweiReleaseState(%d) = %q, want %q", state, got, want)
		}
	}
}

// TestClassifyHuawei locks in the "app's packages exceeds the upper limit"
// classification from https://github.com/KevinGong2013/apkgo/issues/31 —
// an AGC-side draft-version package cap, not an apkgo bug, so it must map
// to config_invalid (human fixes the console, not an auto-retry) rather
// than the generic unknown bucket.
func TestClassifyHuawei(t *testing.T) {
	cases := []struct {
		name string
		ret  retInfo
		want store.Category
	}{
		{
			name: "package limit exceeded",
			ret:  retInfo{Code: 204144662, Message: "[cds]add apk failed, additional msg is [the app's packages exceeds the upper limit.]"},
			want: store.CategoryConfigInvalid,
		},
		{
			name: "same code, unrelated message",
			ret:  retInfo{Code: 204144662, Message: "registeredEntity can not be empty"},
			want: store.CategoryUnknown,
		},
		{
			name: "unrelated code",
			ret:  retInfo{Code: 204144660, Message: "package is parsing"},
			want: store.CategoryUnknown,
		},
	}
	for _, tc := range cases {
		if got := classifyHuawei(tc.ret); got != tc.want {
			t.Errorf("%s: classifyHuawei(%+v) = %q, want %q", tc.name, tc.ret, got, tc.want)
		}
	}
}
