package pgyer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

func init() {
	store.Register("pgyer", store.ConfigSchema{
		Name:       "pgyer",
		ConsoleURL: "https://www.pgyer.com/doc/view/app_upload",
		Fields: []store.FieldSchema{
			{Key: "api_key", Required: true, Desc: "Pgyer API key"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("pgyer", diagnose)
}

// pgyerResp is pgyer's standard envelope. code 0 = success.
type pgyerResp struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type Store struct {
	client *resty.Client
	apiKey string
}

func New(cfg map[string]string) (*Store, error) {
	apiKey := strings.TrimSpace(cfg["api_key"])
	if apiKey == "" {
		return nil, fmt.Errorf("api_key is required")
	}

	client := resty.New().
		SetBaseURL("https://www.pgyer.com/apiv2")

	return &Store{client: client, apiKey: apiKey}, nil
}

func (s *Store) Name() string { return "pgyer" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(ctx context.Context, req *store.UploadRequest) error {
	rep := progress.Safe(req.Progress)

	// 1. Get COS upload token
	rep.Phase("auth")
	var tokenResp struct {
		pgyerResp
		Data struct {
			Key      string            `json:"key"`
			Endpoint string            `json:"endpoint"`
			Params   map[string]string `json:"params"`
		} `json:"data"`
	}

	httpResp, err := s.client.R().
		SetFormData(map[string]string{
			"_api_key":               s.apiKey,
			"buildType":              "apk",
			"buildUpdateDescription": req.ReleaseNotes,
		}).
		SetResult(&tokenResp).
		Post("/app/getCOSToken")
	if err != nil {
		return fmt.Errorf("get cos token: %w", err)
	}
	if httpResp.IsError() {
		return fmt.Errorf("get cos token: http %d: %s", httpResp.StatusCode(), truncateBody(httpResp.String()))
	}
	if tokenResp.Code != 0 {
		return fmt.Errorf("get cos token: [%d] %s", tokenResp.Code, tokenResp.Message)
	}
	if tokenResp.Data.Endpoint == "" || tokenResp.Data.Key == "" {
		return fmt.Errorf("get cos token: empty endpoint/key (raw: %s)", truncateBody(httpResp.String()))
	}

	// 2. Upload to COS endpoint
	tokenResp.Data.Params["key"] = tokenResp.Data.Key

	rep.Phase("uploading")
	rc, size, err := progress.OpenFile(req.FilePath, rep)
	if err != nil {
		return fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	resp, err := httpx.DoMultipart(ctx, httpx.MultipartRequest{
		Method: http.MethodPost,
		URL:    tokenResp.Data.Endpoint,
		Fields: tokenResp.Data.Params,
		Files:  []httpx.FileField{{Field: "file", FileName: filepath.Base(req.FilePath), Reader: rc, Size: size}},
	})
	if err != nil {
		return fmt.Errorf("upload to cos: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload to cos: HTTP %d: %s", resp.StatusCode, truncateBody(string(body)))
	}

	// 3. Poll build info until published
	rep.Phase("processing")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for attempt := 0; attempt < 30; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("processing cancelled (uploaded successfully, check pgyer dashboard): %w", ctx.Err())
		case <-ticker.C:
		}

		var buildResp struct {
			pgyerResp
			Data struct {
				Updated string `json:"buildUpdated"`
			} `json:"data"`
		}

		buildHTTP, err := s.client.R().
			SetQueryParams(map[string]string{
				"_api_key": s.apiKey,
				"buildKey": tokenResp.Data.Key,
			}).
			SetResult(&buildResp).
			Get("/app/buildInfo")
		if err != nil {
			return fmt.Errorf("check build: %w", err)
		}
		if buildHTTP.IsError() {
			return fmt.Errorf("check build: http %d: %s", buildHTTP.StatusCode(), truncateBody(buildHTTP.String()))
		}
		// pgyer keeps reporting code 1247 ("buildKey not found") for the
		// first few seconds while the COS upload is still being ingested.
		// Treat it as "still processing" rather than a hard failure so a
		// successful upload doesn't immediately error out.
		switch buildResp.Code {
		case 0:
			if buildResp.Data.Updated != "" {
				return nil
			}
			// keep polling
		case 1247:
			// still being ingested, keep polling
		default:
			return fmt.Errorf("check build: [%d] %s", buildResp.Code, buildResp.Message)
		}
	}
	return fmt.Errorf("build processing timed out (uploaded successfully, check pgyer dashboard at https://www.pgyer.com/manage/apps)")
}

// truncateBody caps a response body at 500 chars so diagnostic errors stay readable.
func truncateBody(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

// diagnose is registered with `apkgo doctor`. Single probe:
//
//   app-list — POSTs to /app/listMy with the api_key and reports the
//              number of apps under the account. Validates the key
//              without creating any draft uploads.
//
// Pgyer doesn't expose a `/user/info`-style endpoint (returns
// "Unknown method"); /app/listMy is the lightest read-only call that
// exercises the same auth path as the upload endpoints.
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 1)

	s, err := New(cfg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "config", Status: "fail", Error: err.Error()})
		return probes
	}

	var resp struct {
		pgyerResp
		Data struct {
			List []struct {
				BuildName        string `json:"buildName"`
				BuildIdentifier  string `json:"buildIdentifier"`
				BuildVersion     string `json:"buildVersion"`
				BuildVersionNo   string `json:"buildVersionNo"`
			} `json:"list"`
		} `json:"data"`
	}
	httpResp, err := s.client.R().
		SetFormData(map[string]string{"_api_key": s.apiKey}).
		SetResult(&resp).
		Post("/app/listMy")
	if err != nil {
		probes = append(probes, store.Probe{Name: "app-list", Status: "fail", Error: err.Error()})
		return probes
	}
	if httpResp.IsError() {
		probes = append(probes, store.Probe{Name: "app-list", Status: "fail",
			Error: fmt.Sprintf("http %d: %s", httpResp.StatusCode(), truncateBody(httpResp.String()))})
		return probes
	}
	if resp.Code != 0 {
		probes = append(probes, store.Probe{Name: "app-list", Status: "fail",
			Error: fmt.Sprintf("[%d] %s", resp.Code, resp.Message)})
		return probes
	}

	detail := "account active"
	verbose := fmt.Sprintf("%d app(s) under this account", len(resp.Data.List))
	if hint.Package != "" {
		// If a package hint was given, also report whether it's already
		// been uploaded under this account (pgyer keys apps by the
		// Android applicationId in buildIdentifier).
		matched := false
		for _, app := range resp.Data.List {
			if app.BuildIdentifier == hint.Package {
				detail = fmt.Sprintf("%s already uploaded", hint.Package)
				verbose = fmt.Sprintf("%s → %q version=%s build=%s", hint.Package, app.BuildName, app.BuildVersion, app.BuildVersionNo)
				matched = true
				break
			}
		}
		if !matched {
			detail = fmt.Sprintf("%s not yet uploaded (will be created on first push)", hint.Package)
			verbose = fmt.Sprintf("%d app(s) under this account; %s not yet uploaded", len(resp.Data.List), hint.Package)
		}
	}
	probes = append(probes, store.Probe{
		Name:          "app-list",
		Status:        "ok",
		Detail:        detail,
		VerboseDetail: verbose,
	})
	return probes
}
