package apkgo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KevinGong2013/apkgo/v4/pkg/config"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
	"github.com/KevinGong2013/apkgo/v4/pkg/uploader"
)

// testDescStore records the TestDesc Run put on the upload request.
type testDescStore struct{}

func (testDescStore) Name() string { return "test-run-testdesc" }

var (
	testDescMu   sync.Mutex
	testDescSeen string
)

func (testDescStore) Upload(_ context.Context, req *store.UploadRequest) *store.UploadResult {
	testDescMu.Lock()
	testDescSeen = req.TestDesc
	testDescMu.Unlock()
	return store.NewResult("test-run-testdesc", time.Now())
}

func init() {
	store.Register("test-run-testdesc", store.ConfigSchema{
		Name:       "test-run-testdesc",
		AcceptsAAB: true,
	}, func(map[string]string) (store.Store, error) {
		return testDescStore{}, nil
	})
}

// Job.TestDesc is what the stores receive. Config supplies it only when
// the job field is empty, so a per-run value wins over a stored default.
func TestRunOppoTestDesc(t *testing.T) {
	aab := filepath.Join(t.TempDir(), "app.aab")
	if err := os.WriteFile(aab, []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, jobDesc, cfgDesc, want string
	}{
		{"job field wins", "from job", "from config", "from job"},
		{"config when the job field is empty", "", "from config", "from config"},
		{"whitespace job field falls through to config", "  \n", "from config", "from config"},
		{"empty when neither is set", "  ", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testDescMu.Lock()
			testDescSeen = "unset"
			testDescMu.Unlock()

			result, err := Run(context.Background(), Job{
				APKFile:  aab,
				Stores:   []string{"test-run-testdesc"},
				TestDesc: tc.jobDesc,
				Progress: uploader.NopManager,
				Config: &config.Config{Stores: map[string]map[string]string{
					"test-run-testdesc": {},
					"oppo":              {"test_desc": tc.cfgDesc},
				}},
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if len(result.Results) != 1 || !result.Results[0].Success {
				t.Fatalf("results = %+v", result.Results)
			}
			testDescMu.Lock()
			got := testDescSeen
			testDescMu.Unlock()
			if got != tc.want {
				t.Errorf("request TestDesc = %q, want %q", got, tc.want)
			}
		})
	}
}

// JSON credentials (--creds-from) use the same stores.oppo.test_desc key.
func TestRunOppoTestDescFromJSON(t *testing.T) {
	aab := filepath.Join(t.TempDir(), "app.aab")
	if err := os.WriteFile(aab, []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFromJSON(strings.NewReader(`{
		"stores": {
			"test-run-testdesc": {},
			"oppo": {"client_id": "id", "client_secret": "secret", "test_desc": "from json"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	testDescMu.Lock()
	testDescSeen = "unset"
	testDescMu.Unlock()

	if _, err := Run(context.Background(), Job{
		APKFile:  aab,
		Stores:   []string{"test-run-testdesc"},
		Progress: uploader.NopManager,
		Config:   cfg,
	}); err != nil {
		t.Fatal(err)
	}
	testDescMu.Lock()
	got := testDescSeen
	testDescMu.Unlock()
	if got != "from json" {
		t.Errorf("request TestDesc = %q, want %q", got, "from json")
	}
}
