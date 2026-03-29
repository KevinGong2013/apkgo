package oppo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("oppo", store.ConfigSchema{
		Name: "oppo",
		Fields: []store.FieldSchema{
			{Key: "client_id", Required: true, Desc: "OPPO open platform client ID"},
			{Key: "client_secret", Required: true, Desc: "OPPO open platform client secret"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

type Store struct {
	client       *resty.Client
	accessToken  string
	clientSecret string
}

func New(cfg map[string]string) (*Store, error) {
	clientID := cfg["client_id"]
	clientSecret := cfg["client_secret"]
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required")
	}

	client := resty.New().
		SetBaseURL("https://oop-openapi-cn.heytapmobi.com").
		SetHeader("Content-Type", "application/json")

	// Get token
	var resp struct {
		Errno int `json:"errno"`
		Data  struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	_, err := client.R().
		SetQueryParams(map[string]string{
			"client_id":     clientID,
			"client_secret": clientSecret,
		}).
		SetResult(&resp).
		Get("/developer/v1/token")
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	if resp.Errno != 0 || resp.Data.AccessToken == "" {
		return nil, fmt.Errorf("auth failed, errno: %d", resp.Errno)
	}

	return &Store{
		client:       client,
		accessToken:  resp.Data.AccessToken,
		clientSecret: clientSecret,
	}, nil
}

func (s *Store) Name() string { return "oppo" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(ctx context.Context, req *store.UploadRequest) error {
	// 1. Query app info
	app, err := s.queryApp(req.PackageName)
	if err != nil {
		return fmt.Errorf("query app: %w", err)
	}

	// 2. Upload APK
	uploadResult, err := s.uploadAPK(req.FilePath)
	if err != nil {
		return fmt.Errorf("upload apk: %w", err)
	}

	apkInfos := []apkInfo{{URL: uploadResult.URL, MD5: uploadResult.MD5, CpuCode: 0}}

	if req.File64Path != "" {
		apkInfos[0].CpuCode = 32
		result64, err := s.uploadAPK(req.File64Path)
		if err != nil {
			return fmt.Errorf("upload 64-bit apk: %w", err)
		}
		apkInfos = append(apkInfos, apkInfo{URL: result64.URL, MD5: result64.MD5, CpuCode: 64})
	}

	// 3. Publish
	if err := s.publish(req, app, apkInfos); err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	// 4. Poll task state
	return s.pollTaskState(ctx, req.PackageName, strconv.Itoa(int(req.VersionCode)))
}

func (s *Store) queryApp(pkgName string) (*appData, error) {
	data := url.Values{}
	data.Set("pkg_name", pkgName)
	var resp struct {
		Errno int      `json:"errno"`
		Data  *appData `json:"data"`
	}
	_, err := s.client.R().
		SetResult(&resp).
		SetQueryParamsFromValues(s.sign(data)).
		Get("/resource/v1/app/info")
	if err != nil {
		return nil, err
	}
	if resp.Errno != 0 {
		return nil, fmt.Errorf("query errno: %d", resp.Errno)
	}
	return resp.Data, nil
}

func (s *Store) uploadAPK(filePath string) (*uploadResultData, error) {
	// Get upload URL
	var urlResp struct {
		Errno int `json:"errno"`
		Data  struct {
			UploadURL string `json:"upload_url"`
			Sign      string `json:"sign"`
		} `json:"data"`
	}
	_, err := s.client.R().
		SetResult(&urlResp).
		SetQueryParamsFromValues(s.sign(url.Values{})).
		Get("/resource/v1/upload/get-upload-url")
	if err != nil {
		return nil, err
	}

	// Upload file
	var uploadResp struct {
		Errno int              `json:"errno"`
		Data  uploadResultData `json:"data"`
	}
	_, err = s.client.R().
		SetResult(&uploadResp).
		SetFormData(map[string]string{
			"sign": urlResp.Data.Sign,
			"type": "apk",
		}).
		SetFile("file", filePath).
		Post(urlResp.Data.UploadURL)
	if err != nil {
		return nil, err
	}
	return &uploadResp.Data, nil
}

func (s *Store) publish(req *store.UploadRequest, app *appData, apkInfos []apkInfo) error {
	apkJSON, _ := json.Marshal(apkInfos)

	values := url.Values{}
	values.Set("pkg_name", req.PackageName)
	values.Set("version_code", strconv.Itoa(int(req.VersionCode)))
	values.Set("apk_url", string(apkJSON))
	values.Set("app_name", app.AppName)
	values.Set("second_category_id", app.SecondCategoryID)
	values.Set("third_category_id", app.ThirdCategoryID)
	values.Set("summary", app.Summary)
	values.Set("detail_desc", app.DetailDesc)
	values.Set("update_desc", req.ReleaseNotes)
	values.Set("privacy_source_url", app.PrivacySourceURL)
	values.Set("icon_url", app.IconURL)
	values.Set("pic_url", app.PicURL)
	values.Set("online_type", "1")
	values.Set("test_desc", "submitted by apkgo")
	values.Set("copyright_url", app.CopyrightURL)
	values.Set("business_username", app.BusinessUsername)
	values.Set("business_email", app.BusinessEmail)
	values.Set("business_mobile", app.BusinessMobile)
	values.Set("age_level", app.AgeLevel)
	values.Set("adaptive_equipment", app.AdaptiveEquipment)
	values.Set("adaptive_type", "2")
	values.Set("customer_contact", app.CustomerContact)

	var resp struct {
		Errno int `json:"errno"`
		Data  struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	_, err := s.client.R().
		SetResult(&resp).
		SetFormDataFromValues(s.sign(values)).
		Post("/resource/v1/app/upd")
	if err != nil {
		return err
	}
	if resp.Errno != 0 {
		return fmt.Errorf("errno %d: %s", resp.Errno, resp.Data.Message)
	}
	return nil
}

func (s *Store) pollTaskState(ctx context.Context, pkgName, versionCode string) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for attempt := 0; attempt < 10; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		data := url.Values{}
		data.Set("pkg_name", pkgName)
		data.Set("version_code", versionCode)
		var resp struct {
			Errno int `json:"errno"`
			Data  struct {
				TaskState string `json:"task_state"`
				ErrMsg    string `json:"err_msg"`
			} `json:"data"`
		}
		_, err := s.client.R().
			SetResult(&resp).
			SetFormDataFromValues(s.sign(data)).
			Post("/resource/v1/app/task-state")
		if err != nil {
			return err
		}
		switch resp.Data.TaskState {
		case "2": // success
			return nil
		case "3": // failure
			return fmt.Errorf("task failed: %s", resp.Data.ErrMsg)
		}
	}
	return fmt.Errorf("task state polling timed out")
}

// sign adds common params and HMAC-SHA256 signature.
func (s *Store) sign(data url.Values) url.Values {
	data.Set("access_token", s.accessToken)
	data.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))

	// Sort keys and build signing string
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + data.Get(k)
	}
	signStr := strings.Join(parts, "&")

	h := hmac.New(sha256.New, []byte(s.clientSecret))
	h.Write([]byte(signStr))
	data.Set("api_sign", hex.EncodeToString(h.Sum(nil)))

	return data
}

// Data types

type appData struct {
	AppName           string `json:"app_name"`
	SecondCategoryID  string `json:"second_category_id"`
	ThirdCategoryID   string `json:"third_category_id"`
	Summary           string `json:"summary"`
	DetailDesc        string `json:"detail_desc"`
	PrivacySourceURL  string `json:"privacy_source_url"`
	IconURL           string `json:"icon_url"`
	PicURL            string `json:"pic_url"`
	CopyrightURL      string `json:"copyright_url"`
	BusinessUsername  string `json:"business_username"`
	BusinessEmail     string `json:"business_email"`
	BusinessMobile    string `json:"business_mobile"`
	AgeLevel          string `json:"age_level"`
	AdaptiveEquipment string `json:"adaptive_equipment"`
	CustomerContact   string `json:"customer_contact"`
}

type uploadResultData struct {
	URL  string `json:"url"`
	MD5  string `json:"md5"`
	ID   string `json:"id"`
}

type apkInfo struct {
	URL     string `json:"url"`
	MD5     string `json:"md5"`
	CpuCode int    `json:"cpu_code"`
}
