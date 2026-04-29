package store_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/KevinGong2013/apkgo/pkg/store"
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
