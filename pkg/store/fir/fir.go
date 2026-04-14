package fir

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/pkg/progress"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("fir", store.ConfigSchema{
		Name:       "fir",
		ConsoleURL: "https://www.betaqr.com/docs/publish",
		Fields: []store.FieldSchema{
			{Key: "api_token", Required: true, Desc: "fir.im API token"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

type Store struct {
	client   *resty.Client
	apiToken string
}

func New(cfg map[string]string) (*Store, error) {
	apiToken := cfg["api_token"]
	if apiToken == "" {
		return nil, fmt.Errorf("api_token is required")
	}

	client := resty.New().
		SetBaseURL("http://api.bq04.com")

	return &Store{client: client, apiToken: apiToken}, nil
}

func (s *Store) Name() string { return "fir" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(_ context.Context, req *store.UploadRequest) error {
	rep := progress.Safe(req.Progress)

	// 1. Get upload token
	rep.Phase("auth")
	var tokenResp struct {
		Message string `json:"message,omitempty"`
		ID      string `json:"id"`
		Cert    struct {
			Binary struct {
				Key       string `json:"key"`
				Token     string `json:"token"`
				UploadURL string `json:"upload_url"`
			} `json:"binary"`
		} `json:"cert"`
	}

	_, err := s.client.R().
		SetFormData(map[string]string{
			"type":      "android",
			"bundle_id": req.PackageName,
			"api_token": s.apiToken,
		}).
		SetResult(&tokenResp).
		Post("/apps")
	if err != nil {
		return fmt.Errorf("get upload token: %w", err)
	}
	if tokenResp.ID == "" {
		return fmt.Errorf("get upload token: %s", tokenResp.Message)
	}

	// 2. Upload binary
	rep.Phase("uploading")
	rc, _, err := progress.OpenFile(req.FilePath, rep)
	if err != nil {
		return fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	var uploadResp struct {
		Completed bool `json:"is_completed"`
	}

	resp, err := s.client.R().
		SetFormData(map[string]string{
			"key":         tokenResp.Cert.Binary.Key,
			"token":       tokenResp.Cert.Binary.Token,
			"x:name":      req.AppName,
			"x:version":   req.VersionName,
			"x:build":     strconv.Itoa(int(req.VersionCode)),
			"x:changelog": req.ReleaseNotes,
		}).
		SetFileReader("file", filepath.Base(req.FilePath), rc).
		SetResult(&uploadResp).
		Post(tokenResp.Cert.Binary.UploadURL)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	if !uploadResp.Completed {
		return fmt.Errorf("upload incomplete: %s", resp.String())
	}
	return nil
}
