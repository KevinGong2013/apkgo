package samsung

import (
	"testing"

	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

// TestMapSamsungStatus checks the contentStatus keyword mapping, in
// particular that the exact "approved" values win over the broader
// "READY_FOR_"/"UNDER_" review keywords (ordering matters).
func TestMapSamsungStatus(t *testing.T) {
	cases := map[string]store.AuditState{
		"FOR_SALE":                store.AuditApproved,
		"READY_FOR_SALE":          store.AuditApproved, // exact beats the READY_FOR_ review keyword
		"READY_FOR_CHANGE":        store.AuditApproved,
		"SUSPENDED":               store.AuditApproved,
		"UNDER_CONTENT_REVIEW":    store.AuditReviewing,
		"READY_FOR_DEVICE_TEST":   store.AuditReviewing,
		"CONTENT_REVIEW_REJECTED": store.AuditRejected, // REJECTED beats READY_/UNDER_
		"CANCELED":                store.AuditWithdrawn,
		"TERMINATED":              store.AuditWithdrawn,
		"REGISTERING":             store.AuditUnknown,
		"WHATEVER":                store.AuditUnknown,
	}
	for status, want := range cases {
		if got, _ := mapSamsungStatus(status); got != want {
			t.Errorf("mapSamsungStatus(%q) = %q, want %q", status, got, want)
		}
	}
}
