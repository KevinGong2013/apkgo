package oppo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/pkg/progress"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("oppo", store.ConfigSchema{
		Name:       "oppo",
		ConsoleURL: "https://open.oppomobile.com/new/developmentDoc/info?id=10998",
		Fields: []store.FieldSchema{
			{Key: "client_id", Required: true, Desc: "OPPO open platform client ID"},
			{Key: "client_secret", Required: true, Desc: "OPPO open platform client secret"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("oppo", diagnose)
}

// errEnvelope only carries `errno` so it can be embedded in any response
// struct without clashing with the caller's typed `data` field. The human
// message is extracted separately via parseError, which re-unmarshals the
// raw body — OPPO is not consistent about whether the message lives at
// the envelope level (`message` / `msg`) or nested under `data.message`,
// and a generic helper handles all four shapes uniformly.
type errEnvelope struct {
	Errno int `json:"errno"`
}

// parseError pulls a human error message out of the raw response body,
// looking at envelope-level `message`/`msg` first, then `data.message`/
// `data.msg`. Returns the raw body if no recognisable field is present
// so the caller still has something to print.
func parseError(body []byte) string {
	var probe struct {
		Message string `json:"message,omitempty"`
		Msg     string `json:"msg,omitempty"`
		Data    struct {
			Message string `json:"message,omitempty"`
			Msg     string `json:"msg,omitempty"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &probe)
	for _, candidate := range []string{probe.Message, probe.Msg, probe.Data.Message, probe.Data.Msg} {
		if candidate != "" {
			return candidate
		}
	}
	return strings.TrimSpace(string(body))
}

type Store struct {
	client       *resty.Client
	accessToken  string
	clientSecret string
}

func New(cfg map[string]string) (*Store, error) {
	clientID := strings.TrimSpace(cfg["client_id"])
	clientSecret := strings.TrimSpace(cfg["client_secret"])
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required")
	}

	client := resty.New().
		SetBaseURL("https://oop-openapi-cn.heytapmobi.com").
		SetHeader("Content-Type", "application/json")

	token, err := fetchToken(client, clientID, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	return &Store{
		client:       client,
		accessToken:  token,
		clientSecret: clientSecret,
	}, nil
}

// fetchToken exchanges client_id+client_secret for an access token.
// HTTP-level failures and non-zero errno are both surfaced with the
// human-readable message rather than collapsed into a bare error code,
// since OPPO's token errors (e.g. "invalid client_id") are otherwise
// indistinguishable from each other.
func fetchToken(client *resty.Client, clientID, clientSecret string) (string, error) {
	var resp struct {
		errEnvelope
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	httpResp, err := client.R().
		SetQueryParams(map[string]string{
			"client_id":     clientID,
			"client_secret": clientSecret,
		}).
		SetResult(&resp).
		Get("/developer/v1/token")
	if err != nil {
		return "", err
	}
	if httpResp.IsError() {
		return "", fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Errno != 0 || resp.Data.AccessToken == "" {
		return "", fmt.Errorf("[%d] %s", resp.Errno, parseError(httpResp.Body()))
	}
	return resp.Data.AccessToken, nil
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
	rep := progress.Safe(req.Progress)

	// 1. Query app info
	rep.Phase("query")
	app, err := s.queryApp(req.PackageName)
	if err != nil {
		return fmt.Errorf("query app: %w", err)
	}

	// Pre-declare the combined upload size so the bar is stable across
	// apk and apk64 transfers.
	totalBytes, err := sumFileSizes(req.FilePath, req.File64Path)
	if err != nil {
		return fmt.Errorf("stat apk: %w", err)
	}
	rep.Phase("uploading")
	rep.Total(totalBytes)

	// 2. Upload APK
	uploadResult, err := s.uploadAPK(req.FilePath, rep)
	if err != nil {
		return fmt.Errorf("upload apk: %w", err)
	}

	apkInfos := []apkInfo{{URL: uploadResult.URL, MD5: uploadResult.MD5, CpuCode: 0}}

	if req.File64Path != "" {
		apkInfos[0].CpuCode = 32
		result64, err := s.uploadAPK(req.File64Path, rep)
		if err != nil {
			return fmt.Errorf("upload 64-bit apk: %w", err)
		}
		apkInfos = append(apkInfos, apkInfo{URL: result64.URL, MD5: result64.MD5, CpuCode: 64})
	}

	// 3. Publish
	rep.Phase("publishing")
	if err := s.publish(req, app, apkInfos); err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	// 4. Poll task state
	rep.Phase("polling")
	return s.pollTaskState(ctx, req.PackageName, strconv.Itoa(int(req.VersionCode)))
}

// sumFileSizes returns the total size in bytes of the given file paths.
// Empty paths are ignored.
func sumFileSizes(paths ...string) (int64, error) {
	var total int64
	for _, p := range paths {
		if p == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			return 0, err
		}
		total += fi.Size()
	}
	return total, nil
}

func (s *Store) queryApp(pkgName string) (*appData, error) {
	data := url.Values{}
	data.Set("pkg_name", pkgName)
	var resp struct {
		errEnvelope
		Data *appData `json:"data"`
	}
	httpResp, err := s.client.R().
		SetResult(&resp).
		SetQueryParamsFromValues(s.sign(data)).
		Get("/resource/v1/app/info")
	if err != nil {
		return nil, err
	}
	if httpResp.IsError() {
		return nil, fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Errno != 0 {
		return nil, fmt.Errorf("[%d] %s", resp.Errno, parseError(httpResp.Body()))
	}
	return resp.Data, nil
}

func (s *Store) uploadAPK(filePath string, rep progress.Reporter) (*uploadResultData, error) {
	// Get upload URL
	var urlResp struct {
		errEnvelope
		Data struct {
			UploadURL string `json:"upload_url"`
			Sign      string `json:"sign"`
		} `json:"data"`
	}
	httpResp, err := s.client.R().
		SetResult(&urlResp).
		SetQueryParamsFromValues(s.sign(url.Values{})).
		Get("/resource/v1/upload/get-upload-url")
	if err != nil {
		return nil, fmt.Errorf("get-upload-url: %w", err)
	}
	if httpResp.IsError() {
		return nil, fmt.Errorf("get-upload-url: http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if urlResp.Errno != 0 || urlResp.Data.UploadURL == "" {
		return nil, fmt.Errorf("get-upload-url: [%d] %s", urlResp.Errno, parseError(httpResp.Body()))
	}

	// Upload file (streamed, with progress reporting)
	rc, _, err := progress.WrapFile(filePath, rep)
	if err != nil {
		return nil, fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	var uploadResp struct {
		errEnvelope
		Data uploadResultData `json:"data"`
	}
	httpResp, err = s.client.R().
		SetResult(&uploadResp).
		SetFormData(map[string]string{
			"sign": urlResp.Data.Sign,
			"type": "apk",
		}).
		SetFileReader("file", filepath.Base(filePath), rc).
		Post(urlResp.Data.UploadURL)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	if httpResp.IsError() {
		return nil, fmt.Errorf("upload: http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if uploadResp.Errno != 0 {
		return nil, fmt.Errorf("upload: [%d] %s", uploadResp.Errno, parseError(httpResp.Body()))
	}
	if uploadResp.Data.URL == "" {
		return nil, fmt.Errorf("upload: empty url in response (raw: %s)", strings.TrimSpace(string(httpResp.Body())))
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
		errEnvelope
	}
	httpResp, err := s.client.R().
		SetResult(&resp).
		SetFormDataFromValues(s.sign(values)).
		Post("/resource/v1/app/upd")
	if err != nil {
		return err
	}
	if httpResp.IsError() {
		return fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Errno != 0 {
		return fmt.Errorf("[%d] %s", resp.Errno, parseError(httpResp.Body()))
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
			errEnvelope
			Data struct {
				TaskState string `json:"task_state"`
				ErrMsg    string `json:"err_msg"`
			} `json:"data"`
		}
		httpResp, err := s.client.R().
			SetResult(&resp).
			SetFormDataFromValues(s.sign(data)).
			Post("/resource/v1/app/task-state")
		if err != nil {
			return err
		}
		if httpResp.IsError() {
			return fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
		}
		// Surface envelope-level errors immediately. Without this, an
		// auth/sign failure would loop silently for ~100 seconds before
		// reporting a useless "polling timed out".
		if resp.Errno != 0 {
			return fmt.Errorf("[%d] %s", resp.Errno, parseError(httpResp.Body()))
		}
		switch resp.Data.TaskState {
		case "2": // success
			return nil
		case "3": // failure
			return fmt.Errorf("task failed: %s", resp.Data.ErrMsg)
		}
	}
	return fmt.Errorf("task state polling timed out (10 attempts × 10s)")
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

// diagnose is registered with `apkgo doctor`. Probes:
//
//  1. token       — credentials are accepted by /developer/v1/token
//  2. app-info    — the package exists under this developer account, and
//                   the HMAC-SHA256 signature is being computed correctly
//                   (without a sig that lines up server-side, app/info
//                   returns errno=… instead of the package data)
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 2)

	s, err := New(cfg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "token", Status: "fail", Error: err.Error()})
		return probes
	}
	probes = append(probes, store.Probe{Name: "token", Status: "ok", Detail: "access_token issued"})

	if hint.Package == "" {
		probes = append(probes, store.Probe{Name: "app-info", Status: "skip", Detail: "needs --package or --file"})
		return probes
	}

	app, err := s.queryApp(hint.Package)
	if err != nil {
		probes = append(probes, store.Probe{Name: "app-info", Status: "fail", Error: err.Error()})
		return probes
	}
	if app == nil {
		probes = append(probes, store.Probe{Name: "app-info", Status: "fail", Error: fmt.Sprintf("no app found for package %s under this developer account", hint.Package)})
		return probes
	}
	probes = append(probes, store.Probe{Name: "app-info", Status: "ok", Detail: fmt.Sprintf("%s → %q", hint.Package, app.AppName)})
	return probes
}
