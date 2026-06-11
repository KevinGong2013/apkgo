package honor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"context"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

func init() {
	store.Register("honor", store.ConfigSchema{
		Name:                     "honor",
		ConsoleURL:               "https://developer.honor.com/cn/doc/guides/101360",
		SupportsScheduledRelease: true,
		SupportsURLPush:          true,
		Fields: []store.FieldSchema{
			{Key: "client_id", Required: true, Desc: "Honor developer API client ID"},
			{Key: "client_secret", Required: true, Desc: "Honor developer API client secret"},
			{Key: "app_id", Required: false, Desc: "Honor app ID (auto-detected from package name if omitted)"},
			{Key: "url_push_min_mb", Required: false, Desc: "min APK size (MB) to pull from -f URL instead of uploading; honor throttles its download-status poll to ~3min so small files upload faster (default 100)"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("honor", diagnose)
	store.RegisterAuditor("honor", audit)
}

// audit is registered with `apkgo audit`. It reads the package's current
// release/review status via get-app-current-release (which keys off appId
// alone — no releaseId needed), independent of the upload flow.
func audit(ctx context.Context, cfg map[string]string, q store.AuditQuery) store.AuditResult {
	res := store.AuditResult{Store: "honor"}
	s, err := New(cfg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	appID := s.configAppID
	if appID == "" {
		appID, err = s.getAppID(q.Package)
		if err != nil {
			res.Error = err.Error()
			return res
		}
	}
	var resp struct {
		honorResp
		Data struct {
			AuditResult  int    `json:"auditResult"`
			AuditMessage string `json:"auditMessage"`
			VersionName  string `json:"versionName"`
		} `json:"data"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParam("appId", appID).
		SetResult(&resp).
		Get("/openapi/v1/publish/get-app-current-release")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if httpResp.IsError() {
		res.Error = fmt.Sprintf("http %d: %s", httpResp.StatusCode(), truncateBody(httpResp.String()))
		return res
	}
	if resp.Code != 0 {
		res.Error = fmt.Sprintf("[%d] %s", resp.Code, resp.text())
		return res
	}
	res.State, res.Detail = mapHonorAudit(resp.Data.AuditResult, resp.Data.AuditMessage)
	return res
}

// mapHonorAudit maps get-app-current-release auditResult to the unified
// state. 0=审核中, 1=审核通过, 2=审核不通过, 3=其他非审核状态, 4=编辑中未提交.
func mapHonorAudit(auditResult int, msg string) (store.AuditState, string) {
	switch auditResult {
	case 0:
		return store.AuditReviewing, ""
	case 1:
		return store.AuditApproved, ""
	case 2:
		return store.AuditRejected, msg
	case 3:
		return store.AuditUnknown, "non-review state"
	case 4:
		return store.AuditUnknown, "editing, not submitted"
	default:
		return store.AuditUnknown, fmt.Sprintf("auditResult=%d", auditResult)
	}
}

// honorResp is honor's standard response envelope. Code 0 = success.
// Some endpoints use `message` instead of `msg` for the human text;
// text() picks whichever has content so callers don't need to track
// per-endpoint inconsistencies.
type honorResp struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg,omitempty"`
	Message string `json:"message,omitempty"`
}

func (r honorResp) text() string {
	if r.Msg != "" {
		return r.Msg
	}
	return r.Message
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

// Download-mode (upload-by-url) constants. Honor pulls the package from a
// public URL asynchronously and rate-limits status queries to ~once/3min,
// so direct upload is faster for small APKs — we only URL-push above the
// size gate, and poll no more often than Honor allows.
const (
	honorUploadDone        = 0 // upload-by-url status: 0=成功, 1=待上传, 2=上传中
	honorURLPushDefaultMB  = 100
	honorURLPushPollPeriod = 3 * time.Minute
)

type Store struct {
	client          *resty.Client // bound to publishBase with Bearer auth
	accessToken     string        // kept separately so we can pass it on the signed upload URL, which belongs to a different host
	configAppID     string        // optional; when set, skips the get-app-id lookup
	urlPushMinBytes int64         // APK must be at least this big to pull from -f URL; 0 = default
}

func New(cfg map[string]string) (*Store, error) {
	clientID := cfg["client_id"]
	clientSecret := cfg["client_secret"]
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required")
	}

	token, err := fetchToken(clientID, clientSecret)
	if err != nil {
		return nil, store.Categorize(store.CategoryAuthFailed, fmt.Errorf("auth: %w", err))
	}

	client := resty.New().
		SetBaseURL(publishBase).
		SetAuthToken(token).
		SetHeader("Content-Type", "application/json")

	minMB := honorURLPushDefaultMB
	if v := strings.TrimSpace(cfg["url_push_min_mb"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			minMB = n
		}
	}

	return &Store{
		client:          client,
		accessToken:     token,
		configAppID:     cfg["app_id"],
		urlPushMinBytes: int64(minMB) << 20,
	}, nil
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

	// Get the APK to Honor. When -f is a public URL and the APK is large
	// enough to be worth Honor's async download (it throttles status polls
	// to ~once/3min, so small files upload faster directly), hand Honor the
	// URL and let it pull the binary; otherwise upload the bytes.
	if req.SourceURL != "" && s.shouldURLPush(req.FilePath) {
		if err := s.uploadByURL(ctx, appID, req.SourceURL, req.FilePath, rep); err != nil {
			return fmt.Errorf("upload apk by url: %w", err)
		}
	} else if err := s.uploadAPK(ctx, appID, req.FilePath, rep); err != nil {
		return fmt.Errorf("upload apk: %w", err)
	}

	if req.ReleaseNotes != "" {
		rep.Phase("release notes")
		if err := s.updateLanguageInfo(appID, lang, req.ReleaseNotes); err != nil {
			return fmt.Errorf("update release notes: %w", err)
		}
	}

	rep.Phase("submitting")
	return s.submitAudit(appID, req.ReleaseTime)
}

// ---- auth ----

func fetchToken(clientID, clientSecret string) (string, error) {
	httpResp, err := resty.New().R().
		SetFormData(map[string]string{
			"client_id":     clientID,
			"client_secret": clientSecret,
			"grant_type":    "client_credentials",
		}).
		Post(tokenURL)
	if err != nil {
		return "", err
	}
	// Parse the body by hand: resty's SetResult only fills the success
	// struct on 2xx, so a 401 with a perfectly readable
	// {"error":"invalid_client",...} body would leave resp.Error blank
	// and lose the OAuth2 reason.
	body := httpResp.Body()
	var resp struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error,omitempty"`
		ErrorDescription string `json:"error_description,omitempty"`
	}
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return "", fmt.Errorf("decode token response (HTTP %d): %v: %s",
			httpResp.StatusCode(), jerr, truncateBody(string(body)))
	}
	if resp.AccessToken != "" {
		return resp.AccessToken, nil
	}
	if resp.Error != "" {
		return "", fmt.Errorf("[%s] %s", resp.Error, resp.ErrorDescription)
	}
	return "", fmt.Errorf("empty token (HTTP %d): %s", httpResp.StatusCode(), truncateBody(string(body)))
}

// truncateBody caps a response body at 500 chars so diagnostic errors
// stay readable.
func truncateBody(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

// ---- app lookup / detail ----

func (s *Store) getAppID(packageName string) (string, error) {
	// Note: honor returns appId as a JSON number; use int64 and stringify
	// so callers can pass it as a query param verbatim.
	var resp struct {
		honorResp
		Data []struct {
			PackageName string `json:"packageName"`
			AppID       int64  `json:"appId"`
		} `json:"data"`
	}
	httpResp, err := s.client.R().
		SetQueryParam("pkgName", packageName).
		SetResult(&resp).
		Get("/openapi/v1/publish/get-app-id")
	if err != nil {
		return "", err
	}
	if httpResp.IsError() {
		return "", fmt.Errorf("http %d: %s", httpResp.StatusCode(), truncateBody(httpResp.String()))
	}
	if resp.Code != 0 {
		err := fmt.Errorf("[%d] %s", resp.Code, resp.text())
		// 10002 "IAM-TOKEN format error": IAM happily issued a token but
		// the publish gateway doesn't accept it — in practice this means
		// the client_id/client_secret are not AppMarket API client
		// credentials (e.g. some other Honor credential type was pasted
		// in). Point the operator at the right console page, because the
		// raw message suggests a bug rather than a credentials mix-up.
		if resp.Code == 10002 {
			err = fmt.Errorf("[%d] %s（token 已签发但发布接口不认：请确认凭据是「荣耀开发者服务平台 → API 服务」里创建的应用市场 API 客户端 client_id/client_secret）", resp.Code, resp.text())
		}
		return "", store.Categorize(classifyHonor(resp.Code), err)
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
		honorResp
		Data struct {
			LanguageInfo []languageInfo `json:"languageInfo"`
		} `json:"data"`
	}
	httpResp, err := s.client.R().
		SetQueryParam("appId", appID).
		SetResult(&resp).
		Get("/openapi/v1/publish/get-app-detail")
	if err != nil {
		return nil, err
	}
	if httpResp.IsError() {
		return nil, fmt.Errorf("http %d: %s", httpResp.StatusCode(), truncateBody(httpResp.String()))
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("[%d] %s", resp.Code, resp.text())
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
		honorResp
		Data []struct {
			UploadURL string `json:"uploadUrl"`
			ObjectID  int64  `json:"objectId"`
		} `json:"data"`
	}
	httpResp, err := s.client.R().
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
	if httpResp.IsError() {
		return fmt.Errorf("get upload url: http %d: %s", httpResp.StatusCode(), truncateBody(httpResp.String()))
	}
	if urlResp.Code != 0 {
		return fmt.Errorf("get upload url [%d] %s", urlResp.Code, urlResp.text())
	}
	if len(urlResp.Data) == 0 {
		return fmt.Errorf("empty upload url response")
	}
	upload := urlResp.Data[0]

	// Step 3: stream the APK to the signed URL as a multipart POST.
	// Progress.Reader forwards byte counts to the mpb bar.
	rep.Phase("uploading")
	rc, fSize, err := progress.OpenFile(apkPath, rep)
	if err != nil {
		return fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	putHTTP, err := httpx.DoMultipart(ctx, httpx.MultipartRequest{
		Method:  http.MethodPost,
		URL:     upload.UploadURL,
		Headers: map[string]string{"Authorization": "Bearer " + s.accessToken},
		Files:   []httpx.FileField{{Field: "file", FileName: fileName, Reader: rc, Size: fSize}},
	})
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	defer putHTTP.Body.Close()
	putBody, _ := io.ReadAll(putHTTP.Body)
	if putHTTP.StatusCode >= 400 {
		return fmt.Errorf("upload: http %d: %s", putHTTP.StatusCode, truncateBody(string(putBody)))
	}
	// Honor returns a JSON envelope on success; HTTP may be 200 with code!=0
	// when the signed URL rejects the payload (expired nonce, bad sha256).
	var putResp honorResp
	if jerr := json.Unmarshal(putBody, &putResp); jerr != nil {
		return fmt.Errorf("decode upload response (HTTP %d): %v: %s",
			putHTTP.StatusCode, jerr, truncateBody(string(putBody)))
	}
	if putResp.Code != 0 {
		return fmt.Errorf("upload [%d] %s: %s", putResp.Code, putResp.text(), truncateBody(string(putBody)))
	}

	// Step 4: tell honor that objectId is the new binary for this app.
	rep.Phase("publishing")
	return s.bindFile(ctx, appID, upload.ObjectID)
}

// shouldURLPush reports whether the APK is large enough that Honor's async
// download-from-URL (with its ~3min status-poll floor) beats a direct
// upload. Below the threshold, uploading the bytes is faster. A stat
// failure falls back to the upload path.
func (s *Store) shouldURLPush(apkPath string) bool {
	min := s.urlPushMinBytes
	if min <= 0 {
		min = int64(honorURLPushDefaultMB) << 20
	}
	fi, err := os.Stat(apkPath)
	if err != nil {
		return false
	}
	return fi.Size() >= min
}

// uploadByURL hands Honor a public download URL (upload-by-url) instead of
// uploading the APK bytes. Honor downloads the file on its own side
// (async); we poll until it reports the upload finished, then bind the
// objectId exactly as the upload path does. The URL must be HTTPS, public
// and unauthenticated — Honor GETs it directly.
func (s *Store) uploadByURL(ctx context.Context, appID, sourceURL, apkPath string, rep progress.Reporter) error {
	rep.Phase("hashing")
	size, sum, err := statAndSha256(apkPath)
	if err != nil {
		return fmt.Errorf("hash apk: %w", err)
	}
	fileName := filepath.Base(apkPath)

	// Step 1: enqueue the download task (type=1).
	rep.Phase("url push")
	objectID, status, err := s.urlPushTask(ctx, appID, map[string]any{
		"type": 1,
		"uploadList": []map[string]any{{
			"fileName":      fileName,
			"fileType":      fileTypeAPK,
			"fileSize":      size,
			"fileSha256":    sum,
			"fileUploadUrl": sourceURL,
		}},
	})
	if err != nil {
		return fmt.Errorf("create url upload task: %w", err)
	}

	// Step 2: poll until Honor finishes downloading (status 0). Honor
	// rate-limits status queries, so wait ~3min between checks; ctx
	// (the run timeout) bounds the wait.
	for status != honorUploadDone {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for honor to download package from url: %w", ctx.Err())
		case <-time.After(honorURLPushPollPeriod):
		}
		_, status, err = s.urlPushTask(ctx, appID, map[string]any{
			"type":       2,
			"objectList": []map[string]any{{"objectId": objectID}},
		})
		if err != nil {
			return fmt.Errorf("query url upload status: %w", err)
		}
	}

	// Step 3: bind the objectId to the app, same as the upload path.
	rep.Phase("publishing")
	return s.bindFile(ctx, appID, objectID)
}

// urlPushTask POSTs to upload-by-url and returns the first object's id and
// status. Serves both the create (type=1) and status-query (type=2) calls.
func (s *Store) urlPushTask(ctx context.Context, appID string, body map[string]any) (objectID int64, status int, err error) {
	var resp struct {
		honorResp
		Data []struct {
			ObjectID int64 `json:"objectId"`
			Status   int   `json:"status"`
		} `json:"data"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParam("appId", appID).
		SetBody(body).
		SetResult(&resp).
		Post("/openapi/v1/publish/upload-by-url")
	if err != nil {
		return 0, 0, err
	}
	if httpResp.IsError() {
		return 0, 0, fmt.Errorf("http %d: %s", httpResp.StatusCode(), truncateBody(httpResp.String()))
	}
	if resp.Code != 0 {
		return 0, 0, store.Categorize(classifyHonor(resp.Code), fmt.Errorf("[%d] %s", resp.Code, resp.text()))
	}
	if len(resp.Data) == 0 {
		return 0, 0, fmt.Errorf("empty upload-by-url response")
	}
	return resp.Data[0].ObjectID, resp.Data[0].Status, nil
}

// bindFile tells Honor that objectId is the new binary for the app's draft
// version (update-file-info). Shared by the upload and URL-push paths.
func (s *Store) bindFile(ctx context.Context, appID string, objectID int64) error {
	var bindResp honorResp
	bindHTTP, err := s.client.R().
		SetContext(ctx).
		SetQueryParam("appId", appID).
		SetBody(map[string]any{
			"bindingFileList": []map[string]any{{"objectId": objectID}},
		}).
		SetResult(&bindResp).
		Post("/openapi/v1/publish/update-file-info")
	if err != nil {
		return fmt.Errorf("bind file: %w", err)
	}
	if bindHTTP.IsError() {
		return fmt.Errorf("bind file: http %d: %s", bindHTTP.StatusCode(), truncateBody(bindHTTP.String()))
	}
	if bindResp.Code != 0 {
		return fmt.Errorf("bind file [%d] %s", bindResp.Code, bindResp.text())
	}
	return nil
}

// ---- release notes ----

func (s *Store) updateLanguageInfo(appID string, existing *languageInfo, releaseNotes string) error {
	// Honor's update-language-info validates intro / briefIntro as
	// non-empty at request time. If the app on Honor's console has
	// either field blank, we'd hit "[20076] app introduction is empty"
	// after the APK is already uploaded, which is confusing. Fail fast
	// and tell the operator exactly which field to fill in.
	if existing.Intro == "" {
		return store.Categorize(store.CategoryConfigInvalid,
			fmt.Errorf("honor app intro (应用简介) is empty on the console — fill it in before publishing: https://developer.honor.com/cn/console"))
	}

	// Honor's update-language-info blanks out every field it receives as
	// empty, so we re-send appName/intro/briefIntro verbatim and only
	// mutate newFeature.
	var resp honorResp
	httpResp, err := s.client.R().
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
	if httpResp.IsError() {
		return fmt.Errorf("http %d: %s", httpResp.StatusCode(), truncateBody(httpResp.String()))
	}
	if resp.Code != 0 {
		return store.Categorize(classifyHonor(resp.Code),
			fmt.Errorf("[%d] %s", resp.Code, resp.text()))
	}
	return nil
}

// ---- submit ----

func (s *Store) submitAudit(appID string, releaseTime *time.Time) error {
	var resp honorResp
	body := map[string]any{
		"releaseType": 1, // 1 = 全网发布
	}
	if releaseTime != nil {
		// Scheduled release (定时发布): releaseType 2 = 指定时间发布, with
		// releaseTime in UTC format with offset (e.g. 2026-06-20T10:00:00+0800).
		body["releaseType"] = 2
		body["releaseTime"] = releaseTime.Format("2006-01-02T15:04:05Z0700")
	}
	httpResp, err := s.client.R().
		SetQueryParam("appId", appID).
		SetBody(body).
		SetResult(&resp).
		Post("/openapi/v1/publish/submit-audit")
	if err != nil {
		return err
	}
	if httpResp.IsError() {
		return fmt.Errorf("http %d: %s", httpResp.StatusCode(), truncateBody(httpResp.String()))
	}
	if resp.Code != 0 {
		return store.Categorize(classifyHonor(resp.Code),
			fmt.Errorf("[%d] %s", resp.Code, resp.text()))
	}
	return nil
}

// classifyHonor maps known honor response codes to apkgo's
// Category enum. Codes not yet mapped fall through as
// CategoryUnknown — cloud should treat those as not-auto-retryable.
func classifyHonor(code int) store.Category {
	switch code {
	case 10002: // IAM-TOKEN format error — wrong credential type
		return store.CategoryAuthFailed
	case 20076: // app introduction is empty
		return store.CategoryConfigInvalid
	case 20032: // app classification is empty
		return store.CategoryConfigInvalid
	}
	return store.CategoryUnknown
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

// diagnose is registered with `apkgo doctor`. Probes:
//
//  1. token       — credentials exchange for an OAuth2 access token
//  2. get-app-id  — package resolves to an appId under this developer
//     account (skipped if app_id was supplied in config)
//  3. app-detail  — the access token has read access to the app, used
//     to confirm the publish-API permission scope
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 3)

	s, err := New(cfg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "token", Status: "fail", Error: err.Error()})
		return probes
	}
	probes = append(probes, store.Probe{Name: "token", Status: "ok", Detail: "OAuth2 access_token issued"})

	if hint.Package == "" && s.configAppID == "" {
		probes = append(probes,
			store.Probe{Name: "get-app-id", Status: "skip", Detail: "needs --package or --file (or app_id in config)"},
			store.Probe{Name: "app-detail", Status: "skip", Detail: "needs an appId"},
		)
		return probes
	}

	appID := s.configAppID
	if appID == "" {
		appID, err = s.getAppID(hint.Package)
		if err != nil {
			probes = append(probes,
				store.Probe{Name: "get-app-id", Status: "fail", Error: err.Error()},
				store.Probe{Name: "app-detail", Status: "skip", Detail: "needs appId"},
			)
			return probes
		}
		probes = append(probes, store.Probe{Name: "get-app-id", Status: "ok", Detail: fmt.Sprintf("%s → %s", hint.Package, appID)})
	} else {
		probes = append(probes, store.Probe{Name: "get-app-id", Status: "skip", Detail: "using configured app_id=" + appID})
	}

	lang, err := s.getAppLanguage(appID)
	if err != nil {
		probes = append(probes, store.Probe{Name: "app-detail", Status: "fail", Error: err.Error()})
		return probes
	}
	// update-language-info rejects empty intro at request time, so an
	// app whose console-side intro is blank will fail the publish step
	// after the APK is already uploaded. Flag it here so the operator
	// catches the gap before kicking off a real upload.
	if lang.Intro == "" {
		probes = append(probes, store.Probe{Name: "app-detail", Status: "fail",
			Error: fmt.Sprintf("language=%s appName=%q but intro (应用简介) is empty — fill it on the console before publishing", lang.LanguageID, lang.AppName)})
		return probes
	}
	probes = append(probes, store.Probe{Name: "app-detail", Status: "ok",
		Detail: fmt.Sprintf("language=%s appName=%q intro=%d chars", lang.LanguageID, lang.AppName, len(lang.Intro))})
	return probes
}
