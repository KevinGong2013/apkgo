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
