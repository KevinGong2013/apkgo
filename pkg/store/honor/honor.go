package honor

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("honor", store.ConfigSchema{
		Name: "honor",
		Fields: []store.FieldSchema{
			{Key: "client_id", Required: true, Desc: "Honor developer API client ID"},
			{Key: "client_secret", Required: true, Desc: "Honor developer API client secret"},
			{Key: "app_id", Required: true, Desc: "Honor app ID"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

// Honor's API is structurally similar to Huawei AppGallery Connect.
// Base URL: https://appmarket-openapi-drcn.cloud.honor.com

type Store struct {
	client   *resty.Client
	clientID string
	appID    string
}

func New(cfg map[string]string) (*Store, error) {
	clientID := cfg["client_id"]
	clientSecret := cfg["client_secret"]
	appID := cfg["app_id"]
	if clientID == "" || clientSecret == "" || appID == "" {
		return nil, fmt.Errorf("client_id, client_secret, and app_id are required")
	}

	client := resty.New().
		SetBaseURL("https://appmarket-openapi-drcn.cloud.honor.com").
		SetHeader("Content-Type", "application/json")

	s := &Store{client: client, clientID: clientID, appID: appID}

	// Authenticate
	token, err := s.getToken(clientID, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	client.SetAuthToken(token)
	client.SetHeader("client_id", clientID)

	return s, nil
}

func (s *Store) Name() string { return "honor" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(ctx context.Context, req *store.UploadRequest) error {
	if err := s.uploadAPK(req.FilePath); err != nil {
		return fmt.Errorf("upload apk: %w", err)
	}

	// Update release notes
	if req.ReleaseNotes != "" {
		if err := s.updateAppInfo(req.ReleaseNotes); err != nil {
			return fmt.Errorf("update release notes: %w", err)
		}
	}

	return s.pollAndSubmit(ctx)
}

// updateAppInfo sets the release notes for the app.
func (s *Store) updateAppInfo(releaseNotes string) error {
	var resp struct {
		Ret retInfo `json:"ret"`
	}
	_, err := s.client.R().
		SetQueryParams(map[string]string{
			"appId":       s.appID,
			"releaseType": "1",
		}).
		SetBody(map[string]any{
			"newFeatures": releaseNotes,
		}).
		SetResult(&resp).
		Put("/api/publish/v2/app-info")
	if err != nil {
		return err
	}
	if resp.Ret.Code != 0 {
		return fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.Message)
	}
	return nil
}

func (s *Store) getToken(clientID, clientSecret string) (string, error) {
	var resp struct {
		AccessToken string `json:"access_token"`
		Ret         string `json:"ret,omitempty"`
	}
	_, err := s.client.R().
		SetBody(map[string]string{
			"client_id":     clientID,
			"client_secret": clientSecret,
			"grant_type":    "client_credentials",
		}).
		SetResult(&resp).
		Post("/api/oauth2/v1/token")
	if err != nil {
		return "", err
	}
	if resp.AccessToken == "" {
		return "", fmt.Errorf("empty token: %s", resp.Ret)
	}
	return resp.AccessToken, nil
}

func (s *Store) uploadAPK(apkPath string) error {
	// Step 1: Get upload URL
	var urlResp struct {
		Ret      retInfo `json:"ret"`
		URL      string  `json:"uploadUrl"`
		AuthCode string  `json:"authCode"`
	}
	_, err := s.client.R().
		SetQueryParams(map[string]string{
			"appId":       s.appID,
			"releaseType": "1",
			"suffix":      "apk",
		}).
		SetResult(&urlResp).
		Get("/api/publish/v2/upload-url")
	if err != nil {
		return err
	}
	if urlResp.URL == "" {
		return fmt.Errorf("empty upload URL: %s", urlResp.Ret.Message)
	}

	// Step 2: Upload file
	var fileResp struct {
		Result struct {
			UploadFileRsp struct {
				FileInfoList []struct {
					FileDestUlr string `json:"fileDestUlr"`
				} `json:"fileInfoList"`
			} `json:"UploadFileRsp"`
			ResultCode string `json:"resultCode"`
		} `json:"result"`
	}
	filename := filepath.Base(apkPath)
	_, err = resty.New().R().
		SetFile("file", apkPath).
		SetFormData(map[string]string{
			"authCode":  urlResp.AuthCode,
			"fileCount": "1",
			"name":      filename,
			"parseType": "0",
		}).
		SetResult(&fileResp).
		Post(urlResp.URL)
	if err != nil {
		return err
	}
	if fileResp.Result.ResultCode != "0" {
		return fmt.Errorf("upload failed, code: %s", fileResp.Result.ResultCode)
	}
	if len(fileResp.Result.UploadFileRsp.FileInfoList) == 0 {
		return fmt.Errorf("no file info returned")
	}

	// Step 3: Update file info
	var updateResp struct {
		Ret retInfo `json:"ret"`
	}
	_, err = s.client.R().
		SetQueryParams(map[string]string{
			"appId":       s.appID,
			"releaseType": "1",
		}).
		SetBody(map[string]any{
			"fileType": 5,
			"files": []map[string]string{{
				"fileName":    filename,
				"fileDestUrl": fileResp.Result.UploadFileRsp.FileInfoList[0].FileDestUlr,
			}},
		}).
		SetResult(&updateResp).
		Put("/api/publish/v2/app-file-info")
	if err != nil {
		return err
	}
	if updateResp.Ret.Code != 0 {
		return fmt.Errorf("update file info: %s", updateResp.Ret.Message)
	}
	return nil
}

func (s *Store) pollAndSubmit(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for attempt := 0; attempt < 10; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		var resp struct {
			Ret retInfo `json:"ret"`
		}
		s.client.R().
			SetQueryParams(map[string]string{
				"appId":       s.appID,
				"releaseType": "1",
			}).
			SetBody(map[string]any{}).
			SetResult(&resp).
			Post("/api/publish/v2/app-submit")

		if resp.Ret.Code == 0 {
			return nil
		}
	}
	return fmt.Errorf("submit timed out; APK uploaded — submit manually at https://developer.honor.com")
}

type retInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
