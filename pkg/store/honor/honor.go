package honor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"context"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/pkg/progress"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("honor", store.ConfigSchema{
		Name:       "honor",
		ConsoleURL: "https://developer.honor.com/cn/doc/guides/101159",
		Fields: []store.FieldSchema{
			{Key: "client_id", Required: true, Desc: "Honor developer API client ID"},
			{Key: "client_secret", Required: true, Desc: "Honor developer API client secret"},
			{Key: "app_id", Required: false, Desc: "Honor app ID (auto-detected from package name if omitted)"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

// Honor App Market publish API (rewritten 2026-04 to match current endpoints).
// Reference: https://github.com/Xigong93/XiaoZhuan/tree/master/src/main/kotlin/com/xigong/xiaozhuan/channel/honor
//
// Token host:    https://iam.developer.honor.com/auth/token
// Publish host:  https://appmarket-openapi-drcn.cloud.honor.com/openapi/v1/publish

const (
	tokenURL    = "https://iam.developer.honor.com/auth/token"
	publishBase = "https://appmarket-openapi-drcn.cloud.honor.com"
)

// fileTypeAPK is honor's numeric file-type discriminator for APK binaries in
// get-file-upload-url / update-file-info requests.
const fileTypeAPK = 100

type Store struct {
	client      *resty.Client // bound to publishBase with Bearer auth
	accessToken string        // kept separately so we can pass it on the signed upload URL, which belongs to a different host
	configAppID string        // optional; when set, skips the get-app-id lookup
}

func New(cfg map[string]string) (*Store, error) {
	clientID := cfg["client_id"]
	clientSecret := cfg["client_secret"]
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required")
	}

	token, err := fetchToken(clientID, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	client := resty.New().
		SetBaseURL(publishBase).
		SetAuthToken(token).
		SetHeader("Content-Type", "application/json")

	return &Store{client: client, accessToken: token, configAppID: cfg["app_id"]}, nil
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
	rep := progress.Safe(req.Progress)

	rep.Phase("auth")
	appID := s.configAppID
	if appID == "" {
		var err error
		appID, err = s.getAppID(req.PackageName)
		if err != nil {
			return fmt.Errorf("get app id: %w", err)
		}
	}

	// We need the existing appName/intro/briefIntro to echo back verbatim
	// when we PATCH the release notes later — update-language-info will
	// overwrite these fields with empty strings if we don't resend them.
	lang, err := s.getAppLanguage(appID)
	if err != nil {
		return fmt.Errorf("get app detail: %w", err)
	}

	// Upload phase: get signed URL, PUT/POST the APK, bind by objectId.
	if err := s.uploadAPK(ctx, appID, req.FilePath, rep); err != nil {
		return fmt.Errorf("upload apk: %w", err)
	}

	if req.ReleaseNotes != "" {
		rep.Phase("release notes")
		if err := s.updateLanguageInfo(appID, lang, req.ReleaseNotes); err != nil {
			return fmt.Errorf("update release notes: %w", err)
		}
	}

	rep.Phase("submitting")
	return s.submitAudit(appID)
}

// ---- auth ----

func fetchToken(clientID, clientSecret string) (string, error) {
	var resp struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error,omitempty"`
		ErrorDescription string `json:"error_description,omitempty"`
	}
	httpResp, err := resty.New().R().
		SetFormData(map[string]string{
			"client_id":     clientID,
			"client_secret": clientSecret,
			"grant_type":    "client_credentials",
		}).
		SetResult(&resp).
		Post(tokenURL)
	if err != nil {
		return "", err
	}
	if resp.AccessToken != "" {
		return resp.AccessToken, nil
	}
	// Surface the standard OAuth2 error payload when present, else the
	// raw body so unknown schemas stay debuggable.
	if resp.Error != "" {
		return "", fmt.Errorf("%s: %s", resp.Error, resp.ErrorDescription)
	}
	body := strings.TrimSpace(httpResp.String())
	if len(body) > 500 {
		body = body[:500] + "...(truncated)"
	}
	return "", fmt.Errorf("empty token (HTTP %d): %s", httpResp.StatusCode(), body)
}

// ---- app lookup / detail ----

func (s *Store) getAppID(packageName string) (string, error) {
	// Note: honor returns appId as a JSON number; use int64 and stringify
	// so callers can pass it as a query param verbatim.
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			PackageName string `json:"packageName"`
			AppID       int64  `json:"appId"`
		} `json:"data"`
	}
	_, err := s.client.R().
		SetQueryParam("pkgName", packageName).
		SetResult(&resp).
		Get("/openapi/v1/publish/get-app-id")
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("[%d] %s", resp.Code, resp.Msg)
	}
	for _, app := range resp.Data {
		if app.PackageName == packageName && app.AppID != 0 {
			return strconv.FormatInt(app.AppID, 10), nil
		}
	}
	if len(resp.Data) > 0 && resp.Data[0].AppID != 0 {
		return strconv.FormatInt(resp.Data[0].AppID, 10), nil
	}
	return "", fmt.Errorf("no app found for package %s", packageName)
}

// languageInfo is the subset of Honor's languageInfo block that
// update-language-info requires us to echo back. Missing keys would get
// blanked out on the server side.
type languageInfo struct {
	LanguageID string `json:"languageId"`
	AppName    string `json:"appName"`
	Intro      string `json:"intro"`
	BriefIntro string `json:"briefIntro,omitempty"`
}

func (s *Store) getAppLanguage(appID string) (*languageInfo, error) {
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			LanguageInfo []languageInfo `json:"languageInfo"`
		} `json:"data"`
	}
	_, err := s.client.R().
		SetQueryParam("appId", appID).
		SetResult(&resp).
		Get("/openapi/v1/publish/get-app-detail")
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("[%d] %s", resp.Code, resp.Msg)
	}
	if len(resp.Data.LanguageInfo) == 0 {
		return nil, fmt.Errorf("no languageInfo in app detail response")
	}
	// Prefer zh-CN; fall back to whatever is first.
	for i := range resp.Data.LanguageInfo {
		if resp.Data.LanguageInfo[i].LanguageID == "zh-CN" {
			return &resp.Data.LanguageInfo[i], nil
		}
	}
	return &resp.Data.LanguageInfo[0], nil
}

// ---- upload: url → put → bind ----

func (s *Store) uploadAPK(ctx context.Context, appID, apkPath string, rep progress.Reporter) error {
	// Step 1: sha256 + filesize (honor requires them in the get-upload-url body)
	rep.Phase("hashing")
	size, sum, err := statAndSha256(apkPath)
	if err != nil {
		return fmt.Errorf("hash apk: %w", err)
	}

	// Step 2: request an upload URL + objectId for this binary
	fileName := filepath.Base(apkPath)
	var urlResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			UploadURL string `json:"uploadUrl"`
			ObjectID  int64  `json:"objectId"`
		} `json:"data"`
	}
	_, err = s.client.R().
		SetContext(ctx).
		SetQueryParam("appId", appID).
		SetBody([]map[string]any{{
			"fileName":   fileName,
			"fileType":   fileTypeAPK,
			"fileSize":   size,
			"fileSha256": sum,
		}}).
		SetResult(&urlResp).
		Post("/openapi/v1/publish/get-file-upload-url")
	if err != nil {
		return fmt.Errorf("get upload url: %w", err)
	}
	if urlResp.Code != 0 {
		return fmt.Errorf("get upload url [%d] %s", urlResp.Code, urlResp.Msg)
	}
	if len(urlResp.Data) == 0 {
		return fmt.Errorf("empty upload url response")
	}
	upload := urlResp.Data[0]

	// Step 3: stream the APK to the signed URL as a multipart POST.
	// Progress.Reader forwards byte counts to the mpb bar.
	rep.Phase("uploading")
	rc, _, err := progress.OpenFile(apkPath, rep)
	if err != nil {
		return fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	var putResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	putHTTP, err := resty.New().R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+s.accessToken).
		SetFileReader("file", fileName, rc).
		SetResult(&putResp).
		Post(upload.UploadURL)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	// Honor returns a JSON envelope on success; HTTP may be 200 with code!=0
	// when the signed URL rejects the payload (expired nonce, bad sha256).
	if putResp.Code != 0 {
		body := strings.TrimSpace(putHTTP.String())
		if len(body) > 500 {
			body = body[:500] + "...(truncated)"
		}
		return fmt.Errorf("upload [%d] %s: %s", putResp.Code, putResp.Msg, body)
	}

	// Step 4: tell honor that objectId is the new binary for this app.
	rep.Phase("publishing")
	var bindResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_, err = s.client.R().
		SetContext(ctx).
		SetQueryParam("appId", appID).
		SetBody(map[string]any{
			"bindingFileList": []map[string]any{{"objectId": upload.ObjectID}},
		}).
		SetResult(&bindResp).
		Post("/openapi/v1/publish/update-file-info")
	if err != nil {
		return fmt.Errorf("bind file: %w", err)
	}
	if bindResp.Code != 0 {
		return fmt.Errorf("bind file [%d] %s", bindResp.Code, bindResp.Msg)
	}
	return nil
}

// ---- release notes ----

func (s *Store) updateLanguageInfo(appID string, existing *languageInfo, releaseNotes string) error {
	// Honor's update-language-info blanks out every field it receives as
	// empty, so we re-send appName/intro/briefIntro verbatim and only
	// mutate newFeature.
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_, err := s.client.R().
		SetQueryParam("appId", appID).
		SetBody(map[string]any{
			"languageInfoList": []map[string]any{{
				"languageId": existing.LanguageID,
				"appName":    existing.AppName,
				"intro":      existing.Intro,
				"briefIntro": existing.BriefIntro,
				"newFeature": releaseNotes,
			}},
		}).
		SetResult(&resp).
		Post("/openapi/v1/publish/update-language-info")
	if err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("[%d] %s", resp.Code, resp.Msg)
	}
	return nil
}

// ---- submit ----

func (s *Store) submitAudit(appID string) error {
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_, err := s.client.R().
		SetQueryParam("appId", appID).
		SetBody(map[string]any{
			"releaseType": 1, // 1 = 全网发布
		}).
		SetResult(&resp).
		Post("/openapi/v1/publish/submit-audit")
	if err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("[%d] %s", resp.Code, resp.Msg)
	}
	return nil
}

// ---- helpers ----

// statAndSha256 returns the file's byte size and hex-encoded sha256 digest
// in a single pass. Honor's get-file-upload-url endpoint requires both.
func statAndSha256(path string) (size int64, hashHex string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return 0, "", err
	}
	return fi.Size(), hex.EncodeToString(h.Sum(nil)), nil
}
