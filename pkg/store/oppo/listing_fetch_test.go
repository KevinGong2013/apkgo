package oppo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
)

// The listing read maps /app/info's summary / detail_desc / icon_url /
// pic_url, splitting the comma-joined screenshots in order.
func TestRemoteListing(t *testing.T) {
	f := newFakeOppo(t)
	defer f.srv.Close()

	got := f.store().remoteListing(context.Background(), "com.example.app")
	if got.Error != "" {
		t.Fatalf("error: %s", got.Error)
	}
	if got.Store != "oppo" || got.Brief != oldSummary || got.Description != oldDesc || got.Icon != f.srv.URL+"/old/icon.png" {
		t.Errorf("listing = %+v", got)
	}
	if want := strings.Split(oldPicURL, ","); !reflect.DeepEqual(got.Screenshots, want) {
		t.Errorf("screenshots = %v, want %v", got.Screenshots, want)
	}
	if calls, _ := f.seen(); !reflect.DeepEqual(calls, []string{"info"}) {
		t.Errorf("calls = %v, want only the read-only info query", calls)
	}
}

func TestRemoteListingNoApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"errno": 0, "data": []any{}})
	}))
	defer srv.Close()
	s := &Store{client: resty.New().SetBaseURL(srv.URL), accessToken: "t", clientSecret: "s"}

	got := s.remoteListing(context.Background(), "com.missing")
	if !strings.Contains(got.Error, "no app found for package com.missing") {
		t.Errorf("error = %q", got.Error)
	}
}
