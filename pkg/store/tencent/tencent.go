package tencent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("tencent", store.ConfigSchema{
		Name: "tencent",
		Fields: []store.FieldSchema{
			{Key: "user_id", Required: true, Desc: "Tencent open platform user ID"},
			{Key: "access_secret", Required: true, Desc: "API access secret from open.qq.com"},
			{Key: "app_id", Required: true, Desc: "Tencent app ID"},
			{Key: "package_name", Required: true, Desc: "Android package name"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

const baseURL = "https://p.open.qq.com/open_file/developer_api"

type Store struct {
	client       *resty.Client
	userID       string
	accessSecret string
	appID        string
	packageName  string
}

func New(cfg map[string]string) (*Store, error) {
	userID := cfg["user_id"]
	accessSecret := cfg["access_secret"]
	appID := cfg["app_id"]
	packageName := cfg["package_name"]
	if userID == "" || accessSecret == "" || appID == "" || packageName == "" {
		return nil, fmt.Errorf("user_id, access_secret, app_id, and package_name are required")
	}

	client := resty.New().
		SetBaseURL(baseURL).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetTimeout(60 * time.Second)

	return &Store{
		client:       client,
		userID:       userID,
		accessSecret: accessSecret,
		appID:        appID,
		packageName:  packageName,
	}, nil
}

func (s *Store) Name() string { return "tencent" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(ctx context.Context, req *store.UploadRequest) error {
	// 1. Upload APK file → get serial number
	apkSerial, apkMD5, err := s.uploadFile(req.FilePath, "apk")
	if err != nil {
		return fmt.Errorf("upload apk: %w", err)
	}

	// 2. Upload 64-bit APK if provided
	var apk64Serial, apk64MD5 string
	if req.File64Path != "" {
		apk64Serial, apk64MD5, err = s.uploadFile(req.File64Path, "apk")
		if err != nil {
			return fmt.Errorf("upload 64-bit apk: %w", err)
		}
	}

	// 3. Submit update
	if err := s.updateApp(req, apkSerial, apkMD5, apk64Serial, apk64MD5); err != nil {
		return fmt.Errorf("update app: %w", err)
	}

	// 4. Poll audit status
	return s.pollAuditStatus(ctx)
}

// uploadFile gets a pre-signed COS URL, uploads the file, and returns the serial number and file MD5.
func (s *Store) uploadFile(filePath, fileType string) (serialNumber string, fileMd5 string, err error) {
	fileName := filepath.Base(filePath)

	// Get upload info
	params := url.Values{}
	params.Set("pkg_name", s.packageName)
	params.Set("app_id", s.appID)
	params.Set("file_type", fileType)
	params.Set("file_name", fileName)

	var resp struct {
		Ret          int    `json:"ret"`
		Msg          string `json:"msg"`
		PreSignURL   string `json:"pre_sign_url"`
		SerialNumber string `json:"serial_number"`
	}
	if err := s.post("/get_file_upload_info", params, &resp); err != nil {
		return "", "", err
	}
	if resp.Ret != 0 {
		return "", "", fmt.Errorf("[%d] %s", resp.Ret, resp.Msg)
	}

	// Calculate MD5
	fileMd5, err = calcFileMD5(filePath)
	if err != nil {
		return "", "", fmt.Errorf("calc md5: %w", err)
	}

	// Upload file to COS via pre-signed URL (PUT with raw bytes)
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", fmt.Errorf("read file: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPut, resp.PreSignURL, bytes.NewReader(fileContent))
	if err != nil {
		return "", "", fmt.Errorf("create put request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/octet-stream")

	httpClient := &http.Client{Timeout: 5 * time.Minute}
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", "", fmt.Errorf("upload to cos: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return "", "", fmt.Errorf("cos upload failed: HTTP %d: %s", httpResp.StatusCode, string(body))
	}

	return resp.SerialNumber, fileMd5, nil
}

// updateApp submits the app update with APK serial numbers and metadata.
func (s *Store) updateApp(req *store.UploadRequest, apkSerial, apkMD5, apk64Serial, apk64MD5 string) error {
	params := url.Values{}
	params.Set("pkg_name", s.packageName)
	params.Set("app_id", s.appID)
	params.Set("deploy_type", "1") // publish immediately after approval

	// APK files
	if apk64Serial != "" {
		// Split arch: 32-bit + 64-bit
		params.Set("apk32_flag", "1")
		params.Set("apk32_file_serial_number", apkSerial)
		params.Set("apk32_file_md5", apkMD5)
		params.Set("apk64_flag", "1")
		params.Set("apk64_file_serial_number", apk64Serial)
		params.Set("apk64_file_md5", apk64MD5)
	} else {
		// Single APK (32&64 compatible)
		params.Set("apk32_flag", "1")
		params.Set("apk32_file_serial_number", apkSerial)
		params.Set("apk32_file_md5", apkMD5)
	}

	// Release notes
	if req.ReleaseNotes != "" {
		params.Set("feature", req.ReleaseNotes)
	}

	var resp struct {
		Ret int    `json:"ret"`
		Msg string `json:"msg"`
	}
	if err := s.post("/update_app", params, &resp); err != nil {
		return err
	}
	if resp.Ret != 0 {
		return fmt.Errorf("[%d] %s", resp.Ret, resp.Msg)
	}
	return nil
}

// pollAuditStatus checks the audit status until resolved or timeout.
func (s *Store) pollAuditStatus(ctx context.Context) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for attempt := 0; attempt < 20; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		params := url.Values{}
		params.Set("pkg_name", s.packageName)
		params.Set("app_id", s.appID)

		var resp struct {
			Ret         int    `json:"ret"`
			Msg         string `json:"msg"`
			AuditStatus int    `json:"audit_status"`
			AuditReason string `json:"audit_reason"`
		}
		if err := s.post("/query_app_update_status", params, &resp); err != nil {
			return err
		}

		switch resp.AuditStatus {
		case 3: // approved
			return nil
		case 2: // rejected
			return fmt.Errorf("audit rejected: %s", resp.AuditReason)
		case 8: // withdrawn
			return fmt.Errorf("audit withdrawn")
		}
		// status 1 = auditing, continue polling
	}

	// Timeout is not an error — the update was submitted successfully
	return nil
}

// post makes a signed POST request to the Tencent API.
func (s *Store) post(path string, params url.Values, result any) error {
	// Add common params
	params.Set("user_id", s.userID)
	params.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))

	// Calculate sign
	params.Set("sign", s.calcSign(params))

	resp, err := s.client.R().
		SetBody(params.Encode()).
		Post(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(resp.Body(), result)
}

// calcSign computes HMAC-SHA256 signature over sorted params.
func (s *Store) calcSign(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "sign" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + params.Get(k)
	}
	signStr := strings.Join(parts, "&")

	h := hmac.New(sha256.New, []byte(s.accessSecret))
	h.Write([]byte(signStr))
	return hex.EncodeToString(h.Sum(nil))
}

func calcFileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
