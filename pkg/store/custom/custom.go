package custom

import (
	"context"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("custom", store.ConfigSchema{
		Name: "custom",
		Fields: []store.FieldSchema{
			{Key: "url", Required: true, Desc: "Upload endpoint URL"},
			{Key: "method", Required: false, Desc: "HTTP method (default: POST)"},
			{Key: "field_name", Required: false, Desc: "Multipart file field name (default: file)"},
			// Headers are passed as header_<name> keys, e.g. header_Authorization
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

type Store struct {
	url       string
	method    string
	fieldName string
	headers   map[string]string
}

func New(cfg map[string]string) (*Store, error) {
	u := cfg["url"]
	if u == "" {
		return nil, fmt.Errorf("url is required")
	}

	method := cfg["method"]
	if method == "" {
		method = "POST"
	}

	fieldName := cfg["field_name"]
	if fieldName == "" {
		fieldName = "file"
	}

	// Extract headers: keys starting with "header_"
	headers := map[string]string{}
	for k, v := range cfg {
		if len(k) > 7 && k[:7] == "header_" {
			headers[k[7:]] = v
		}
	}

	return &Store{
		url:       u,
		method:    method,
		fieldName: fieldName,
		headers:   headers,
	}, nil
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
	r := resty.New().R().
		SetFile(s.fieldName, req.FilePath).
		SetFormData(map[string]string{
			"app_name":      req.AppName,
			"package_name":  req.PackageName,
			"version_name":  req.VersionName,
			"version_code":  fmt.Sprintf("%d", req.VersionCode),
			"release_notes": req.ReleaseNotes,
		})

	for k, v := range s.headers {
		r.SetHeader(k, v)
	}

	resp, err := r.Execute(s.method, s.url)
	if err != nil {
		return err
	}
	if resp.StatusCode() >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode(), resp.String())
	}
	return nil
}
