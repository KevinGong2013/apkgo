package huawei

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

func init() {
	store.Register("huawei", store.ConfigSchema{
		Name:                     "huawei",
		ConsoleURL:               "https://developer.huawei.com/consumer/cn/doc/AppGallery-connect-Guides/agcapi-getstarted-0000001111845114#section1785535363715",
		SupportsScheduledRelease: true,
		SupportsURLPush:          true,
		Fields: []store.FieldSchema{
			{Key: "service_account", Required: false, Desc: "Service Account credential JSON (raw or base64); recommended"},
			{Key: "service_account_file", Required: false, Desc: "Path to Service Account credential JSON file"},
			{Key: "client_id", Required: false, Desc: "[deprecated] API client ID — Huawei is migrating to Service Account"},
			{Key: "client_secret", Required: false, Desc: "[deprecated] API client secret"},
			{Key: "app_id", Required: false, Desc: "Huawei app ID (auto-detected from package name if omitted)"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("huawei", diagnose)
	store.RegisterAuditor("huawei", audit)
}

// audit is registered with `apkgo audit`. It reads the app's releaseState
// via the read-only app-info query (GET), mapping it to the unified review
// state — independent of the upload flow.
func audit(ctx context.Context, cfg map[string]string, q store.AuditQuery) store.AuditResult {
	res := store.AuditResult{Store: "huawei"}
	s, err := New(cfg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	appID := s.configAppID
	if appID == "" {
		appID, err = s.fetchAppID(q.Package)
		if err != nil {
			res.Error = err.Error()
			return res
		}
	}
	var resp struct {
		Ret          retInfo `json:"ret"`
		ReleaseState int     `json:"releaseState"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{"appId": appID, "releaseType": "1"}).
		SetResult(&resp).
		Get("/api/publish/v2/app-info")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if httpResp.IsError() {
		res.Error = fmt.Sprintf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
		return res
	}
	if resp.Ret.Code != 0 {
		res.Error = fmt.Sprintf("[%d] %s", resp.Ret.Code, resp.Ret.text())
		return res
	}
	res.State, res.Detail = mapHuaweiReleaseState(resp.ReleaseState)
	return res
}

// mapHuaweiReleaseState maps app-info releaseState (releaseType=1) to the
// unified state. 0=已上架, 1=上架审核不通过, 2=已下架, 3=待上架(预约), 4=审核中,
// 5=升级审核中, 6=申请下架, 7=草稿, 8=升级审核不通过, 9=下架审核不通过,
// 10=开发者下架, 11=撤销上架, 12=预审中, 13=预审不通过.
func mapHuaweiReleaseState(state int) (store.AuditState, string) {
	switch state {
	case 4, 5, 12:
		return store.AuditReviewing, fmt.Sprintf("releaseState=%d", state)
	case 0, 3:
		return store.AuditApproved, fmt.Sprintf("releaseState=%d", state)
	case 1, 8, 13:
		return store.AuditRejected, fmt.Sprintf("releaseState=%d", state)
	case 2, 10, 11:
		return store.AuditWithdrawn, fmt.Sprintf("releaseState=%d", state)
	case 7:
		return store.AuditUnknown, "draft (草稿)"
	default:
		return store.AuditUnknown, fmt.Sprintf("releaseState=%d", state)
	}
}

// authMode reflects which credential type is in effect; used by diagnostics.
type authMode int

const (
	authNone authMode = iota
	authServiceAccount
	authClientCredentials
)

type Store struct {
	client      *resty.Client
	clientID    string // for client_credentials mode; empty under service_account
	configAppID string
	mode        authMode
}

func New(cfg map[string]string) (*Store, error) {
	saInline := strings.TrimSpace(cfg["service_account"])
	saFile := strings.TrimSpace(cfg["service_account_file"])
	clientID := strings.TrimSpace(cfg["client_id"])
	clientSecret := strings.TrimSpace(cfg["client_secret"])

	client := resty.New().
		SetBaseURL("https://connect-api.cloud.huawei.com").
		SetHeader("Content-Type", "application/json")

	s := &Store{client: client, configAppID: cfg["app_id"]}

	switch {
	case saInline != "" || saFile != "":
		sa, key, err := func() (*serviceAccount, *rsa.PrivateKey, error) {
			if saInline != "" {
				return loadServiceAccount(saInline)
			}
			return loadServiceAccountFromFile(saFile)
		}()
		if err != nil {
			return nil, fmt.Errorf("auth: %w", err)
		}
		jwt, err := signJWT(sa, key, time.Now())
		if err != nil {
			return nil, fmt.Errorf("auth: sign jwt: %w", err)
		}
		// Per Huawei docs the signed JWT IS the access token. No client_id
		// header needed.
		client.SetAuthToken(jwt)
		s.mode = authServiceAccount
	case clientID != "" && clientSecret != "":
		token, err := s.getToken(clientID, clientSecret)
		if err != nil {
			return nil, fmt.Errorf("auth: %w", err)
		}
		client.SetAuthToken(token)
		client.SetHeader("client_id", clientID)
		s.clientID = clientID
		s.mode = authClientCredentials
	default:
		return nil, fmt.Errorf("huawei: configure service_account (recommended) or client_id+client_secret")
	}

	return s, nil
}

func (s *Store) Name() string { return "huawei" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()

	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(ctx context.Context, req *store.UploadRequest) error {
	rep := progress.Safe(req.Progress)

	// Resolve app ID
	rep.Phase("auth")
	appID := s.configAppID
	if appID == "" {
		var err error
		appID, err = s.fetchAppID(req.PackageName)
		if err != nil {
			return fmt.Errorf("fetch app_id: %w", err)
		}
	}

	// Get the APK to Huawei. When -f was a public URL, hand Huawei the URL
	// and let it download the package itself (skips re-uploading the
	// bytes); otherwise upload the local file. The by-url path is async —
	// pollAndSubmit below already retries while Huawei is still parsing,
	// which absorbs the download wait.
	if req.SourceURL != "" {
		rep.Phase("submitting url")
		if err := s.submitPackageByURL(appID, req.SourceURL, req); err != nil {
			return fmt.Errorf("submit package by url: %w", err)
		}
	} else if err := s.uploadAPK(appID, req.FilePath, rep); err != nil {
		return fmt.Errorf("upload apk: %w", err)
	}

	// Update release notes (newFeatures)
	if req.ReleaseNotes != "" {
		rep.Phase("release notes")
		if err := s.updateAppInfo(appID, req.ReleaseNotes); err != nil {
			return fmt.Errorf("update release notes: %w", err)
		}
	}

	// Poll for compilation readiness then submit
	rep.Phase("submitting")
	if err := s.pollAndSubmit(ctx, appID, req.ReleaseTime); err != nil {
		return err
	}
	return nil
}

// updateAppInfo sets the release notes (newFeatures) for the app.
func (s *Store) updateAppInfo(appID, releaseNotes string) error {
	var resp struct {
		Ret retInfo `json:"ret"`
	}
	_, err := s.client.R().
		SetQueryParams(map[string]string{
			"appId":       appID,
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
		return fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text())
	}
	return nil
}

// getToken exchanges credentials for an access token.
//
// Huawei's response shape on success has no "ret" field; on failure
// it returns `{"ret":{"code":<int>,"msg":"<string>"}}` with HTTP 200.
// The struct field for ret must therefore be the object form, not a
// string — otherwise unmarshaling masks the real error message.
func (s *Store) getToken(clientID, clientSecret string) (string, error) {
	var resp struct {
		AccessToken string `json:"access_token"`
		Ret         struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		} `json:"ret"`
	}
	httpResp, err := s.client.R().
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
	if httpResp.IsError() {
		return "", fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.AccessToken == "" {
		if resp.Ret.Code != 0 || resp.Ret.Msg != "" {
			return "", fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.Msg)
		}
		return "", fmt.Errorf("empty token (raw: %s)", strings.TrimSpace(string(httpResp.Body())))
	}
	return resp.AccessToken, nil
}

// fetchAppID resolves package name to Huawei app ID.
func (s *Store) fetchAppID(packageName string) (string, error) {
	var resp struct {
		Ret    retInfo `json:"ret"`
		AppIds []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"appIds"`
	}
	httpResp, err := s.client.R().
		SetQueryParam("packageName", packageName).
		SetQueryParam("packageTypes", "1").
		SetResult(&resp).
		Get("/api/publish/v2/appid-list")
	if err != nil {
		return "", err
	}
	if httpResp.IsError() {
		// Surface the HTTP failure verbatim — Huawei's CDN returns 403 with
		// an empty body (and no `ret` JSON) when the API key lacks scope
		// for this app/project, which would otherwise be silently mistaken
		// for "package not found".
		msg := strings.TrimSpace(string(httpResp.Body()))
		if msg == "" {
			msg = httpResp.Status()
		}
		return "", fmt.Errorf("http %d: %s", httpResp.StatusCode(), msg)
	}
	if resp.Ret.Code != 0 {
		return "", fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text())
	}
	if len(resp.AppIds) == 0 {
		return "", fmt.Errorf("no app found for package %s", packageName)
	}
	return resp.AppIds[0].Value, nil
}

// uploadAPK handles the 3-step file upload: get URL → upload → update file info.
func (s *Store) uploadAPK(appID, apkPath string, rep progress.Reporter) error {
	// Step 1: Get upload URL
	url, authCode, err := s.getUploadURL(appID)
	if err != nil {
		return err
	}

	// Step 2: Upload file to the URL
	var fileResp struct {
		Result struct {
			UploadFileRsp struct {
				IfSuccess    int `json:"ifSuccess"`
				FileInfoList []struct {
					FileDestUlr string `json:"fileDestUlr"`
				} `json:"fileInfoList"`
			} `json:"UploadFileRsp"`
			ResultCode string `json:"resultCode"`
		} `json:"result"`
	}
	filename := filepath.Base(apkPath)
	rep.Phase("uploading")
	rc, fSize, err := progress.OpenFile(apkPath, rep)
	if err != nil {
		return fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()
	resp, err := httpx.DoMultipart(context.Background(), httpx.MultipartRequest{
		Method: http.MethodPost,
		URL:    url,
		Fields: map[string]string{
			"authCode":  authCode,
			"fileCount": "1",
			"name":      filename,
			"parseType": "0",
		},
		Files: []httpx.FileField{{Field: "file", FileName: filename, Reader: rc, Size: fSize}},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("upload failed: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if jerr := json.Unmarshal(body, &fileResp); jerr != nil {
		return fmt.Errorf("decode upload response (HTTP %d): %v: %s",
			resp.StatusCode, jerr, strings.TrimSpace(string(body)))
	}
	if fileResp.Result.ResultCode != "0" {
		return fmt.Errorf("upload failed, resultCode: %s", fileResp.Result.ResultCode)
	}
	if len(fileResp.Result.UploadFileRsp.FileInfoList) == 0 {
		return fmt.Errorf("no file info returned after upload")
	}

	// Step 3: Update file info
	var updateResp struct {
		Ret retInfo `json:"ret"`
	}
	_, err = s.client.R().
		SetQueryParams(map[string]string{
			"appId":       appID,
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
		return fmt.Errorf("update file info: [%d] %s", updateResp.Ret.Code, updateResp.Ret.text())
	}
	return nil
}

// submitPackageByURL hands Huawei a developer-hosted download URL
// (POST /publish/v2/app-package-file/by-url) instead of uploading the APK
// bytes. Huawei downloads the package from the URL on its own side and
// associates it with the app's draft version — no fileDestUrl is returned
// to bind, so unlike the upload path there is no app-file-info step. The
// download is asynchronous: this call only enqueues it (ret.code 0), and
// the subsequent pollAndSubmit absorbs the wait via its parsing-retry.
// The URL must be publicly GET-able (Huawei fetches it unauthenticated).
func (s *Store) submitPackageByURL(appID, sourceURL string, req *store.UploadRequest) error {
	// downloadFileName must carry the real suffix; derive it from the URL
	// path, falling back to "<package>.apk".
	name := sourceURL
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		name = req.PackageName + ".apk"
	}
	requestID := fmt.Sprintf("apkgo-%s-%d-%d", req.PackageName, req.VersionCode, time.Now().UnixNano())

	var resp struct {
		Ret retInfo `json:"ret"`
	}
	httpResp, err := s.client.R().
		SetQueryParams(map[string]string{"appId": appID}).
		SetBody(map[string]any{
			"downloadUrl":      sourceURL,
			"downloadFileName": name,
			"requestId":        requestID,
			"packageType":      1, // 1 = APK
		}).
		SetResult(&resp).
		Post("/api/publish/v2/app-package-file/by-url")
	if err != nil {
		return err
	}
	if httpResp.IsError() {
		return fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Ret.Code != 0 {
		return fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text())
	}
	return nil
}

// hwConsoleURL is the AGC release console — by the time pollAndSubmit
// runs, the APK is already on Huawei's side, so every submit-step
// failure should point the operator here to finish the release manually.
const hwConsoleURL = "https://developer.huawei.com/consumer/cn/console"

// pollAndSubmit tries to submit the app for review, polling if Huawei is
// still parsing the APK. Huawei's parse step can take ~1–2 minutes for
// large binaries but is sometimes instant, so we attempt submit once
// immediately and only enter the polling loop if the server reports the
// package as still parsing.
//
// Every failure path adds the "APK already uploaded; finish review at <url>"
// hint, since the APK is on Huawei's side by the time we get here and the
// operator's recovery action is identical regardless of why submit failed.
//
// Note: code 204144660 is a generic "submit failed" code — the real reason
// lives in `msg`. Real configuration errors (e.g. missing publisher entity)
// also come back as 204144660, so we must inspect the message and only
// retry the "still parsing" subset; otherwise we'd burn 5 minutes hiding
// an error the operator can fix immediately.
func (s *Store) pollAndSubmit(ctx context.Context, appID string, releaseTime *time.Time) error {
	wrap := func(format string, args ...any) error {
		return fmt.Errorf(format+" (APK 已上传成功，请在 AGC 后台完成审核：%s)", append(args, hwConsoleURL)...)
	}

	// Immediate attempt so fast-parsing APKs don't wait 30s for nothing.
	ret, err := s.submitApp(appID, releaseTime)
	if err != nil {
		return wrap("submit: %v", err)
	}
	if ret.Code == 0 {
		return nil
	}
	if !isParsingInProgress(ret) {
		return wrap("submit: [%d] %s", ret.Code, ret.text())
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Huawei's "being compiled" error explicitly says "try again in 3-5
	// minutes", and parsing for large APKs is similar; 14 × 30s ≈ 7 min
	// gives buffer over both bounds while staying inside the default
	// 10-minute global timeout.
	for attempt := 0; attempt < 14; attempt++ {
		select {
		case <-ctx.Done():
			return wrap("submit phase timed out: %v", ctx.Err())
		case <-ticker.C:
		}

		ret, err := s.submitApp(appID, releaseTime)
		if err != nil {
			return wrap("submit: %v", err)
		}
		if ret.Code == 0 {
			return nil
		}
		if !isParsingInProgress(ret) {
			return wrap("submit: [%d] %s", ret.Code, ret.text())
		}
	}
	return wrap("submit timed out after 14 attempts (APK still being processed by Huawei)")
}

// isParsingInProgress detects the transient "package still being processed"
// case so we know to retry. Huawei has at least two codes here:
//
//   - 204144660 with msg containing "parsing/parse/解析" — APK still being
//     parsed (metadata extraction)
//   - 204144727 with msg containing "being compiled" / "3-5 minutes" — APK
//     being recompiled on Huawei's side
//
// Both codes are also reused for non-transient errors (e.g. 204144660 also
// covers "registeredEntity can not be empty"), so we must match the message
// substring rather than the code alone.
func isParsingInProgress(ret retInfo) bool {
	msg := strings.ToLower(ret.text())
	switch ret.Code {
	case 204144660:
		return strings.Contains(msg, "parsing") || strings.Contains(msg, "parse") || strings.Contains(msg, "解析")
	case 204144727:
		return strings.Contains(msg, "compil") || strings.Contains(msg, "编译") || strings.Contains(msg, "try again")
	}
	return false
}

// submitApp posts to /app-submit and returns the parsed `ret` object.
// HTTP-level failures (transport error, non-2xx with no parseable ret)
// are surfaced as the second return value rather than being silently
// converted into a zero-valued ret, which would look like success.
func (s *Store) submitApp(appID string, releaseTime *time.Time) (retInfo, error) {
	var resp struct {
		Ret retInfo `json:"ret"`
	}
	query := map[string]string{
		"appId":       appID,
		"releaseType": "1",
	}
	if releaseTime != nil {
		// Scheduled release (定时发布): Huawei's releaseTime query param,
		// UTC format with offset (e.g. 2026-06-20T10:00:00+0800). When
		// omitted the app goes live immediately after the review passes.
		query["releaseTime"] = releaseTime.Format("2006-01-02T15:04:05Z0700")
	}
	httpResp, err := s.client.R().
		SetQueryParams(query).
		SetBody(map[string]any{}).
		SetResult(&resp).
		Post("/api/publish/v2/app-submit")
	if err != nil {
		return retInfo{}, err
	}
	if httpResp.IsError() {
		return retInfo{}, fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	return resp.Ret, nil
}

// getUploadURL fetches a per-upload destination URL + auth code from Huawei.
// Reused by both the upload path and `apkgo doctor` to verify release perms.
func (s *Store) getUploadURL(appID string) (uploadURL, authCode string, err error) {
	var resp struct {
		Ret      retInfo `json:"ret"`
		URL      string  `json:"uploadUrl"`
		AuthCode string  `json:"authCode"`
	}
	httpResp, err := s.client.R().
		SetQueryParams(map[string]string{
			"appId":       appID,
			"releaseType": "1",
			"suffix":      "apk",
		}).
		SetResult(&resp).
		Get("/api/publish/v2/upload-url")
	if err != nil {
		return "", "", err
	}
	if httpResp.IsError() {
		return "", "", fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.URL == "" {
		return "", "", fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text())
	}
	return resp.URL, resp.AuthCode, nil
}

// diagnose is registered with `apkgo doctor`. It runs three layered probes:
//
//  1. token              — credentials are accepted (catches wrong client_id type)
//  2. appid-list         — package name resolves to an appId in this AGC team
//  3. release-permission — upload-url returns a destination, which the API
//     only does when the API key has "App release" rights
//
// Probes 2 and 3 require a package name (DiagnoseHint.Package). They are
// reported as skipped when that hint is absent.
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 3)

	s, err := New(cfg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "token", Status: "fail", Error: err.Error()})
		return probes
	}
	mode := "unknown"
	switch s.mode {
	case authServiceAccount:
		mode = "service_account (PS256 JWT)"
	case authClientCredentials:
		mode = "client_credentials (deprecated by Huawei)"
	}
	probes = append(probes, store.Probe{Name: "token", Status: "ok", Detail: "auth mode: " + mode})

	if hint.Package == "" {
		probes = append(probes,
			store.Probe{Name: "appid-list", Status: "skip", Detail: "needs --package or --file"},
			store.Probe{Name: "release-permission", Status: "skip", Detail: "needs --package or --file"},
		)
		return probes
	}

	appID := s.configAppID
	if appID == "" {
		appID, err = s.fetchAppID(hint.Package)
		if err != nil {
			probes = append(probes,
				store.Probe{Name: "appid-list", Status: "fail", Error: err.Error()},
				store.Probe{Name: "release-permission", Status: "skip", Detail: "needs appid-list"},
			)
			return probes
		}
		probes = append(probes, store.Probe{Name: "appid-list", Status: "ok", Detail: fmt.Sprintf("%s → %s", hint.Package, appID)})
	} else {
		probes = append(probes, store.Probe{Name: "appid-list", Status: "skip", Detail: "using configured app_id=" + appID})
	}

	if _, _, err := s.getUploadURL(appID); err != nil {
		probes = append(probes, store.Probe{Name: "release-permission", Status: "fail", Error: err.Error()})
		return probes
	}
	probes = append(probes, store.Probe{Name: "release-permission", Status: "ok", Detail: "upload-url issued (App release permission granted)"})

	return probes
}

// retInfo is Huawei's standard response envelope. The message field is
// returned as `message` on some endpoints and `msg` on others (token,
// appid-list, app-submit, …) — both are populated and the helper picks
// whichever has content.
type retInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

// text returns whichever of message / msg is non-empty.
func (r retInfo) text() string {
	if r.Message != "" {
		return r.Message
	}
	return r.Msg
}
