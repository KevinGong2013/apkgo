package samsung

import (
	"reflect"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// Orchestrators retry a network failure by calling Upload again on the same
// Store. Once Add Binary has succeeded, the retry must not add the binary a
// second time — Samsung answers that with 5021 on every further attempt.
func TestRetryAfterSubmitFailureDoesNotAddBinaryAgain(t *testing.T) {
	f := newFakeSamsung(t)
	f.submit = []string{"drop"}
	s := f.store()
	req := &store.UploadRequest{FilePath: writeAPK(t, t.TempDir()), VersionCode: 6}

	first := s.Upload(t.Context(), req)
	if first.Success || first.Category != store.CategoryNetworkRetry {
		t.Fatalf("first attempt: success=%v category=%q (%s); want a network_retry failure", first.Success, first.Category, first.Error)
	}

	f.calls = nil
	if res := s.Upload(t.Context(), req); !res.Success {
		t.Fatalf("retry: %s", res.Error)
	}
	want := []string{
		"/seller/createUploadSessionId", "/seller/contentInfo",
		"/seller/contentUpdate", "/seller/contentSubmit",
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("retry calls = %v, want %v (no re-upload, no second Add Binary)", f.calls, want)
	}
	if !f.submitted {
		t.Fatal("retry did not submit")
	}
}

// Add Binary got through but its answer was lost: the retry's 5021 means
// the earlier attempt's binary is attached, so the retry submits.
func TestRetryAfterLostAddBinaryAnswer(t *testing.T) {
	f := newFakeSamsung(t)
	f.binary = []string{"drop", "inuse"}
	s := f.store()
	req := &store.UploadRequest{FilePath: writeAPK(t, t.TempDir()), VersionCode: 6}

	first := s.Upload(t.Context(), req)
	if first.Success || first.Category != store.CategoryNetworkRetry {
		t.Fatalf("first attempt: success=%v category=%q (%s); want a network_retry failure", first.Success, first.Category, first.Error)
	}
	if res := s.Upload(t.Context(), req); !res.Success {
		t.Fatalf("retry: %s", res.Error)
	}
	if !f.submitted {
		t.Fatal("retry did not submit")
	}
}

// A versionCode attached by an earlier run (another Store instance, e.g. a
// later job) can't be told apart from a live binary or another build, so it
// is not submitted blindly: the error says what to do, and nothing is
// submitted.
func TestBinaryAlreadyAttachedByEarlierRun(t *testing.T) {
	f := newFakeSamsung(t)
	f.info = `[{"contentStatus":"REGISTERING","defaultLanguageCode":"CHI","paid":"N","binaryList":[` +
		`{"versionCode":"40002","gms":"N","binarySeq":"7"},{"versionCode":"40003","gms":"N","binarySeq":"8"}]}]`
	f.binary = []string{"inuse"}
	req := &store.UploadRequest{FilePath: writeAPK(t, t.TempDir()), VersionCode: 40003}

	res := f.store().Upload(t.Context(), req)
	if res.Success || res.Category != store.CategoryConfigInvalid {
		t.Fatalf("success=%v category=%q (%s); want a config_invalid failure", res.Success, res.Category, res.Error)
	}
	for _, want := range []string{"versionCode 40003", "Seller Portal", "5021", "This binary is already in use"} {
		if !strings.Contains(res.Error, want) {
			t.Errorf("error = %q, want it to mention %q", res.Error, want)
		}
	}
	if f.submitted {
		t.Fatal("submitted despite Add Binary failing")
	}
}

// Without a binary of this versionCode in contentInfo there is no telling
// what the 5021 is about: the store's answer is passed on unchanged.
func TestBinaryInUseWithoutDuplicateKeepsStoreError(t *testing.T) {
	f := newFakeSamsung(t)
	f.binary = []string{"inuse"}
	req := &store.UploadRequest{FilePath: writeAPK(t, t.TempDir()), VersionCode: 6}

	res := f.store().Upload(t.Context(), req)
	if res.Success || res.Category != store.CategoryUnknown {
		t.Fatalf("success=%v category=%q (%s); want an unknown failure", res.Success, res.Category, res.Error)
	}
	if !strings.HasPrefix(res.Error, "add binary: http 400: ") || strings.Contains(res.Error, "Seller Portal") {
		t.Errorf("error = %q, want Samsung's answer unchanged", res.Error)
	}
}

// contentSubmit can refuse with a 200 and a non-zero resultCode; that must
// not pass for a submission.
func TestSubmitResultCode(t *testing.T) {
	f := newFakeSamsung(t)
	f.submit = []string{`{"resultCode":"3201","resultMessage":"The application status is not REGISTERING"}`}
	req := &store.UploadRequest{FilePath: writeAPK(t, t.TempDir()), VersionCode: 6}

	res := f.store().Upload(t.Context(), req)
	if res.Success || !strings.Contains(res.Error, "submit failed: 3201") {
		t.Fatalf("success=%v error=%q; want the submit failure", res.Success, res.Error)
	}
}

func TestHasBinary(t *testing.T) {
	info := map[string]any{"binaryList": []any{
		map[string]any{"versionCode": "40002"},
		map[string]any{"versionCode": float64(40003)},
	}}
	if !hasBinary(info, 40003) || !hasBinary(info, 40002) || hasBinary(info, 40004) || hasBinary(map[string]any{}, 1) {
		t.Fatal("hasBinary mismatched")
	}
}
