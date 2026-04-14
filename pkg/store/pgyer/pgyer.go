package pgyer

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/pkg/progress"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("pgyer", store.ConfigSchema{
		Name:       "pgyer",
		ConsoleURL: "https://www.pgyer.com/account/api",
		Fields: []store.FieldSchema{
			{Key: "api_key", Required: true, Desc: "Pgyer API key"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

type Store struct {
	client *resty.Client
	apiKey string
}

func New(cfg map[string]string) (*Store, error) {
	apiKey := cfg["api_key"]
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
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Key      string            `json:"key"`
			Endpoint string            `json:"endpoint"`
			Params   map[string]string `json:"params"`
		} `json:"data"`
	}

	_, err := s.client.R().
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
	if tokenResp.Code != 0 {
		return fmt.Errorf("get cos token: %s", tokenResp.Message)
	}

	// 2. Upload to COS endpoint
	tokenResp.Data.Params["key"] = tokenResp.Data.Key

	rep.Phase("uploading")
	rc, _, err := progress.OpenFile(req.FilePath, rep)
	if err != nil {
		return fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	resp, err := s.client.R().
		SetFormData(tokenResp.Data.Params).
		SetFileReader("file", filepath.Base(req.FilePath), rc).
		Post(tokenResp.Data.Endpoint)
	if err != nil {
		return fmt.Errorf("upload to cos: %w", err)
	}
	if resp.StatusCode() != 204 {
		return fmt.Errorf("upload to cos: HTTP %d", resp.StatusCode())
	}

	// 3. Poll build info until published
	rep.Phase("processing")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for attempt := 0; attempt < 30; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		var buildResp struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				Updated string `json:"buildUpdated"`
			} `json:"data"`
		}

		_, err := s.client.R().
			SetQueryParams(map[string]string{
				"_api_key": s.apiKey,
				"buildKey": tokenResp.Data.Key,
			}).
			SetResult(&buildResp).
			Get("/app/buildInfo")
		if err != nil {
			return fmt.Errorf("check build: %w", err)
		}
		if buildResp.Code == 1216 {
			return fmt.Errorf("build failed: %s", buildResp.Message)
		}
		if buildResp.Data.Updated != "" {
			return nil
		}
	}
	return fmt.Errorf("build processing timed out (uploaded successfully, check pgyer dashboard)")
}
