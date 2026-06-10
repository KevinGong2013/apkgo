package store_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

func TestCategorize(t *testing.T) {
	if got := store.Categorize(store.CategoryAuthFailed, nil); got != nil {
		t.Errorf("Categorize(nil) = %v, want nil", got)
	}

	base := errors.New("token expired")
	wrapped := store.Categorize(store.CategoryAuthFailed, base)
	if got := store.CategoryOf(wrapped); got != store.CategoryAuthFailed {
		t.Errorf("CategoryOf wrapped = %s, want auth_failed", got)
	}
	if got := wrapped.Error(); got != "token expired" {
		t.Errorf("wrapped.Error() = %q, want %q", got, "token expired")
	}
	if !errors.Is(wrapped, base) {
		t.Error("errors.Is(wrapped, base) = false, want true")
	}

	// fmt.Errorf %w wrapping should still be detectable
	deep := fmt.Errorf("upload phase: %w", wrapped)
	if got := store.CategoryOf(deep); got != store.CategoryAuthFailed {
		t.Errorf("CategoryOf deep = %s, want auth_failed", got)
	}

	// nil err → success
	if got := store.CategoryOf(nil); got != store.CategorySuccess {
		t.Errorf("CategoryOf(nil) = %s, want success", got)
	}

	// uncategorised → unknown
	if got := store.CategoryOf(errors.New("plain")); got != store.CategoryUnknown {
		t.Errorf("CategoryOf plain = %s, want unknown", got)
	}
}

func TestCategoryOfNetworkFallback(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want store.Category
	}{
		{"deadline exceeded", fmt.Errorf("upload to cos: %w", context.DeadlineExceeded), store.CategoryNetworkRetry},
		{"url.Error", fmt.Errorf("upload: %w", &url.Error{Op: "Post", URL: "https://api.example.com/upload", Err: errors.New("use of closed network connection")}), store.CategoryNetworkRetry},
		{"closed connection string", errors.New(`upload apk: write tcp 1.2.3.4:46098->5.6.7.8:443: use of closed network connection`), store.CategoryNetworkRetry},
		{"client timeout string", errors.New("context deadline exceeded (Client.Timeout exceeded while awaiting headers)"), store.CategoryNetworkRetry},
		{"app-level rejection stays unknown", errors.New("[911216] 版本更新任务处理中"), store.CategoryUnknown},
		// An explicit category from the store wins over the fallback even
		// when the message looks network-ish.
		{"explicit category wins", store.Categorize(store.CategoryAuthFailed, errors.New("token refresh: connection reset by peer")), store.CategoryAuthFailed},
	}
	for _, tc := range cases {
		if got := store.CategoryOf(tc.err); got != tc.want {
			t.Errorf("%s: CategoryOf = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestAlreadyDoneError(t *testing.T) {
	ad := &store.AlreadyDoneError{Reason: "in review"}
	if !store.IsAlreadyDone(ad) {
		t.Error("IsAlreadyDone(direct) = false")
	}
	if !store.IsAlreadyDone(fmt.Errorf("wrapped: %w", ad)) {
		t.Error("IsAlreadyDone(wrapped) = false")
	}
	if store.IsAlreadyDone(errors.New("plain")) {
		t.Error("IsAlreadyDone(plain) = true")
	}
	if got := ad.Error(); got != "already done: in review" {
		t.Errorf("Error() = %q", got)
	}
}
