package vivo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

type traceLog struct {
	mu  sync.Mutex
	got []httptrace.Exchange
}

func (l *traceLog) Record(ex httptrace.Exchange) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, ex)
}

// A store created under a recorder has its whole upload recorded — the
// streamed APK as its name and size only — and labelled with the name it
// was configured under.
func TestUploadIsRecorded(t *testing.T) {
	answer := func(method, body string) httptrace.Exchange {
		return httptrace.Exchange{
			Method: "POST", URL: "https://developer-api.vivo.com.cn/router/rest?method=" + method, Status: 200,
			Response: &httptrace.Message{Headers: map[string]string{"Content-Type": "application/json"}, JSON: json.RawMessage(body)},
		}
	}
	rp := httptrace.Replay([]httptrace.Exchange{
		answer("app.upload.apk.app", `{"code":0,"subCode":"0","data":{"packageName":"com.example.app","serialnumber":"sn-1","fileMd5":"abc"}}`),
		answer("app.sync.update.app", `{"code":0,"subCode":"0"}`),
	}).MatchQuery("method")
	rec := &traceLog{}
	ctx := httptrace.WithRecorder(httptrace.WithReplay(context.Background(), rp), rec)

	apk := filepath.Join(t.TempDir(), "app-release.apk")
	payload := strings.Repeat("apk-bytes-", 5000)
	if err := os.WriteFile(apk, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.CreateContext(ctx, "vivo", map[string]string{"access_key": "key", "access_secret": "topsecret"})
	if err != nil {
		t.Fatal(err)
	}
	res := s.Upload(ctx, &store.UploadRequest{
		FilePath: apk, PackageName: "com.example.app", VersionCode: 7, VersionName: "1.0.7", ReleaseNotes: "修复已知问题",
	})
	if !res.Success {
		t.Fatalf("upload failed: %s", res.Error)
	}

	if len(rec.got) != 2 || len(rp.Remaining()) != 0 {
		t.Fatalf("%d exchanges recorded, %d replies unused", len(rec.got), len(rp.Remaining()))
	}
	up, update := rec.got[0], rec.got[1]
	if up.Store != "vivo" || update.Store != "vivo" || up.Status != 200 {
		t.Errorf("labels/status: %+v", up)
	}
	mp := up.Request.Multipart
	if mp == nil || len(mp.Files) != 1 || mp.Files[0].Name != "app-release.apk" || mp.Files[0].Size != int64(len(payload)) || mp.Files[0].Field != "file" {
		t.Fatalf("upload recorded as %+v", up.Request)
	}
	if up.Request.Text != "" || up.Request.JSON != nil {
		t.Error("file bytes found their way into the record")
	}
	if string(up.Response.JSON) != `{"code":0,"data":{"fileMd5":"abc","packageName":"com.example.app","serialnumber":"sn-1"},"subCode":"0"}` {
		t.Errorf("upload response = %s", up.Response.JSON)
	}
	for _, want := range []string{"method=app.sync.update.app", "apk=sn-1", "versionCode=7", "sign=%5BREDACTED"} {
		if !strings.Contains(update.URL, want) {
			t.Errorf("update request %s lacks %s", update.URL, want)
		}
	}
	line, _ := json.Marshal(rec.got)
	if strings.Contains(string(line), "topsecret") || strings.Contains(string(line), "apk-bytes-") {
		t.Error("secret or file content in the record")
	}

	// Without a recorder the same store is built with untouched clients.
	plain, err := store.Create("vivo", map[string]string{"access_key": "key", "access_secret": "topsecret"})
	if err != nil {
		t.Fatal(err)
	}
	if v := plain.(*Store); v.uploadClient != nil {
		t.Error("untraced store got a dedicated upload client")
	}
}
