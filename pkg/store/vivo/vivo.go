package vivo

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("vivo", store.ConfigSchema{
		Name: "vivo",
		Fields: []store.FieldSchema{
			{Key: "access_key", Required: true, Desc: "vivo open platform access key"},
			{Key: "access_secret", Required: true, Desc: "vivo open platform access secret"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

type Store struct {
	client       *resty.Client
	accessKey    string
	accessSecret []byte
}

func New(cfg map[string]string) (*Store, error) {
	accessKey := cfg["access_key"]
	accessSecret := cfg["access_secret"]
	if accessKey == "" || accessSecret == "" {
		return nil, fmt.Errorf("access_key and access_secret are required")
	}

	client := resty.New().
		SetBaseURL("https://developer-api.vivo.com.cn/router/rest")

	return &Store{
		client:       client,
		accessKey:    accessKey,
		accessSecret: []byte(accessSecret),
	}, nil
}

func (s *Store) Name() string { return "vivo" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(_ context.Context, req *store.UploadRequest) error {
	updateReq := map[string]string{
		"packageName":      req.PackageName,
		"versionCode":      strconv.Itoa(int(req.VersionCode)),
		"onlineType":       "1",
		"updateDesc":       req.ReleaseNotes,
		"compatibleDevice": "2",
	}

	if req.File64Path != "" {
		// Split package upload
		resp32, err := s.uploadAPK("app.upload.apk.app.32", req.PackageName, req.FilePath)
		if err != nil {
			return fmt.Errorf("upload 32-bit: %w", err)
		}
		resp64, err := s.uploadAPK("app.upload.apk.app.64", req.PackageName, req.File64Path)
		if err != nil {
			return fmt.Errorf("upload 64-bit: %w", err)
		}
		updateReq["apk32"] = resp32.SerialNumber
		updateReq["apk64"] = resp64.SerialNumber
		return s.updateApp("app.sync.update.subpackage.app", updateReq)
	}

	// Single package upload
	resp, err := s.uploadAPK("app.upload.apk.app", req.PackageName, req.FilePath)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	updateReq["apk"] = resp.SerialNumber
	updateReq["fileMd5"] = resp.FileMD5
	return s.updateApp("app.sync.update.app", updateReq)
}

func (s *Store) uploadAPK(method, packageName, filePath string) (*uploadResp, error) {
	fileMd5, err := fileMD5(filePath)
	if err != nil {
		return nil, err
	}

	params := s.signParams(method, map[string]string{
		"packageName": packageName,
		"fileMd5":     fileMd5,
	})

	var resp struct {
		Code    int         `json:"code"`
		Message string      `json:"msg"`
		Data    *uploadResp `json:"data"`
	}
	_, err = s.client.R().
		SetFile("file", filePath).
		SetQueryParams(params).
		SetResult(&resp).
		Post("")
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("[%d] %s", resp.Code, resp.Message)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("empty response data")
	}
	return resp.Data, nil
}

func (s *Store) updateApp(method string, bizParams map[string]string) error {
	params := s.signParams(method, bizParams)

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_, err := s.client.R().
		SetQueryParams(params).
		SetResult(&resp).
		Post("")
	if err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("[%d] %s", resp.Code, resp.Message)
	}
	return nil
}

// signParams builds the full param map with HMAC-SHA256 signature.
func (s *Store) signParams(method string, bizParams map[string]string) map[string]string {
	params := map[string]string{
		"method":         method,
		"access_key":     s.accessKey,
		"timestamp":      strconv.FormatInt(time.Now().UnixMilli(), 10),
		"format":         "json",
		"v":              "1.0",
		"sign_method":    "HMAC-SHA256",
		"target_app_key": "developer",
	}
	for k, v := range bizParams {
		params[k] = v
	}

	// Sort and concatenate
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}

	h := hmac.New(sha256.New, s.accessSecret)
	h.Write([]byte(sb.String()))
	params["sign"] = hex.EncodeToString(h.Sum(nil))

	return params
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	io.Copy(h, f)
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

type uploadResp struct {
	PackageName  string `json:"packageName"`
	SerialNumber string `json:"serialnumber"`
	VersionCode  string `json:"versionCode"`
	FileMD5      string `json:"fileMd5"`
}
