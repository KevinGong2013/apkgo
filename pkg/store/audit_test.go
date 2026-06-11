package store_test

import (
	"testing"

	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

// TestAuditStateResolved locks in which states `apkgo audit --watch`
// treats as terminal (stop polling) vs still-in-flight.
func TestAuditStateResolved(t *testing.T) {
	resolved := []store.AuditState{store.AuditApproved, store.AuditRejected, store.AuditWithdrawn}
	for _, s := range resolved {
		if !s.Resolved() {
			t.Errorf("%q.Resolved() = false, want true", s)
		}
	}
	pending := []store.AuditState{store.AuditReviewing, store.AuditUnknown, store.AuditState("")}
	for _, s := range pending {
		if s.Resolved() {
			t.Errorf("%q.Resolved() = true, want false", s)
		}
	}
}
