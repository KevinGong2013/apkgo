package custom

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("custom", store.ConfigSchema{
		Name: "custom",
		Fields: []store.FieldSchema{
			{Key: "upload_url", Required: true, Desc: "File upload endpoint URL"},
			{Key: "upload_method", Required: false, Desc: "Upload HTTP method (default: POST)"},
			{Key: "upload_field", Required: false, Desc: "Multipart file field name (default: file)"},
			{Key: "publish_url", Required: false, Desc: "Publish/release endpoint URL (if separate from upload)"},
			{Key: "publish_method", Required: false, Desc: "Publish HTTP method (default: POST)"},
			// Headers: header_<name> applies to both; upload_header_<name> / publish_header_<name> for per-step
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

type Store struct {
	uploadURL    string
	uploadMethod string
	uploadField  string
	publishURL   string
	publishMethod string
	commonHeaders  map[string]string
	uploadHeaders  map[string]string
	publishHeaders map[string]string
}

func New(cfg map[string]string) (*Store, error) {
	uploadURL := cfg["upload_url"]
	// Backward compat: accept "url" as alias for "upload_url"
	if uploadURL == "" {
		uploadURL = cfg["url"]
	}
	if uploadURL == "" {
		return nil, fmt.Errorf("upload_url is required")
	}

	s := &Store{
		uploadURL:      uploadURL,
		uploadMethod:   or(cfg["upload_method"], cfg["method"], "POST"),
		uploadField:    or(cfg["upload_field"], cfg["field_name"], "file"),
		publishURL:     cfg["publish_url"],
		publishMethod:  or(cfg["publish_method"], "POST"),
		commonHeaders:  extractHeaders(cfg, "header_"),
		uploadHeaders:  extractHeaders(cfg, "upload_header_"),
		publishHeaders: extractHeaders(cfg, "publish_header_"),
	}
	return s, nil
}

func (s *Store) Name() string { return "custom" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(_ context.Context, req *store.UploadRequest) error {
	// Step 1: Upload file
	uploadResp, err := s.doUpload(req)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	// Step 2: Publish (if separate endpoint configured)
	if s.publishURL != "" {
		if err := s.doPublish(req, uploadResp); err != nil {
			return fmt.Errorf("publish: %w", err)
		}
	}

	return nil
}

func (s *Store) doUpload(req *store.UploadRequest) (string, error) {
	r := resty.New().R().
		SetFile(s.uploadField, req.FilePath).
		SetFormData(map[string]string{
			"app_name":     req.AppName,
			"package_name": req.PackageName,
			"version_name": req.VersionName,
			"version_code": fmt.Sprintf("%d", req.VersionCode),
		})

	// If no separate publish step, include release notes in upload
	if s.publishURL == "" {
		r.SetFormData(map[string]string{"release_notes": req.ReleaseNotes})
	}

	for k, v := range s.commonHeaders {
		r.SetHeader(k, v)
	}
	for k, v := range s.uploadHeaders {
		r.SetHeader(k, v)
	}

	resp, err := r.Execute(s.uploadMethod, s.uploadURL)
	if err != nil {
		return "", err
	}
	if resp.StatusCode() >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode(), resp.String())
	}
	return resp.String(), nil
}

func (s *Store) doPublish(req *store.UploadRequest, uploadResponse string) error {
	// Try to extract file_id/file_key from upload response JSON
	var respData map[string]any
	json.Unmarshal([]byte(uploadResponse), &respData)

	body := map[string]string{
		"package_name":  req.PackageName,
		"version_name":  req.VersionName,
		"version_code":  fmt.Sprintf("%d", req.VersionCode),
		"release_notes": req.ReleaseNotes,
	}

	// Forward common response fields (file_id, file_key, file_url, id, key)
	for _, key := range []string{"file_id", "file_key", "file_url", "id", "key", "url"} {
		if v, ok := respData[key]; ok {
			body[key] = fmt.Sprintf("%v", v)
		}
	}

	r := resty.New().R().SetFormData(body)

	for k, v := range s.commonHeaders {
		r.SetHeader(k, v)
	}
	for k, v := range s.publishHeaders {
		r.SetHeader(k, v)
	}

	resp, err := r.Execute(s.publishMethod, s.publishURL)
	if err != nil {
		return err
	}
	if resp.StatusCode() >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode(), resp.String())
	}
	return nil
}

func extractHeaders(cfg map[string]string, prefix string) map[string]string {
	headers := map[string]string{}
	for k, v := range cfg {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			headers[k[len(prefix):]] = v
		}
	}
	return headers
}

func or(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
