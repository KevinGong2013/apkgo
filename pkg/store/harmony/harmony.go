// Package harmony publishes HarmonyOS (鸿蒙 / HarmonyOS NEXT) apps and
// atomic services to Huawei AppGallery through the AppGallery Connect
// Publishing API.
//
// It shares credentials and the API host with the Android `huawei`
// store (one AGC Service Account covers both), but the HarmonyOS flow is
// a distinct set of endpoints:
//
//	GET  /api/publish/v2/appid-list?packageTypes=7     bundleName → appId
//	GET  /api/publish/v2/upload-url/for-obs            signed PUT target (objectId)
//	PUT  <urlInfo.url> (+ urlInfo.headers)             raw .app bytes
//	PUT  /api/publish/v3/app-package-info              bind objectId → packageId
//	PUT  /api/publish/v3/app-language-info             newFeatures (release notes)
//	GET  /api/publish/v3/package/compile/status        wait for server-side parse
//	POST /api/publish/v3/app-submit                    submit for review
//	GET  /api/publish/v3/app-info                      review status (apkgo audit)
//
// Only `.app` App Packs are accepted by AGC for release; a bare `.hap`
// module is rejected up-front with guidance.
package harmony

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v3/pkg/apk"
	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
	"github.com/KevinGong2013/apkgo/v3/pkg/store/huawei"
)

// StoreName is the registry name of this store.
const StoreName = "harmony"

// packageTypeHarmony is AGC's packageTypes filter value for HarmonyOS
// apps / atomic services on the appid-list endpoint.
const packageTypeHarmony = "7"

// consoleURL is the AGC release console — once the package is on
// Huawei's side every later failure points the operator here.
const consoleURL = "https://developer.huawei.com/consumer/cn/service/josp/agc/index.html"

func init() {
	store.Register(StoreName, store.ConfigSchema{
		Name:                     StoreName,
		ConsoleURL:               "https://developer.huawei.com/consumer/cn/doc/app/agc-help-connect-api-obtain-server-auth-0000002271134661",
		Platform:                 store.PlatformHarmony,
		SupportsScheduledRelease: true,
		Fields: append(append([]store.FieldSchema{}, huawei.CredentialFields...),
			store.FieldSchema{Key: "app_id", Required: false, Desc: "AGC app ID of the HarmonyOS app (auto-detected from bundleName if omitted)"},
			store.FieldSchema{Key: "lang", Required: false, Desc: "Language of the release notes, e.g. zh-CN (default: the app's default language in AGC)"},
			store.FieldSchema{Key: "chinese_mainland_flag", Required: false, Desc: "Set to 1 (distribute in Chinese mainland) or 0 when the developer is registered outside mainland China; AGC requires it in that case"},
		),
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser(StoreName, diagnose)
	store.RegisterAuditor(StoreName, audit)
}

// Store publishes HarmonyOS .app packs via AGC.
type Store struct {
	client         *resty.Client
	upload         *http.Client // raw PUT to the signed OBS URL
	mode           huawei.AuthMode
	configAppID    string
	lang           string
	mainlandFlag   string
	pollInterval   time.Duration
	compileTimeout time.Duration
}

// New builds a store from the flat config map (see the schema in init).
func New(cfg map[string]string) (*Store, error) {
	client, mode, err := huawei.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("harmony: %w", err)
	}
	return &Store{
		client:         client,
		upload:         &http.Client{Timeout: 30 * time.Minute},
		mode:           mode,
		configAppID:    strings.TrimSpace(cfg["app_id"]),
		lang:           strings.TrimSpace(cfg["lang"]),
		mainlandFlag:   strings.TrimSpace(cfg["chinese_mainland_flag"]),
		pollInterval:   20 * time.Second,
		compileTimeout: 8 * time.Minute,
	}, nil
}

func (s *Store) Name() string { return StoreName }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	packageID, err := s.publish(ctx, req)
	if err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	res := store.NewResult(s.Name(), start)
	res.ExternalID = packageID
	return res
}

func (s *Store) publish(ctx context.Context, req *store.UploadRequest) (string, error) {
	rep := progress.Safe(req.Progress)

	if !apk.IsHarmonyAppPack(req.FilePath) {
		if apk.IsHarmony(req.FilePath) {
			return "", store.Categorize(store.CategoryConfigInvalid,
				fmt.Errorf("AGC only accepts .app App Packs for HarmonyOS releases, got a bare .hap module — build the app with DevEco Studio (Build → Build Hap(s)/APP(s) → Build APP(s)) and upload the signed .app"))
		}
		return "", store.Categorize(store.CategoryConfigInvalid,
			fmt.Errorf("harmony store needs a HarmonyOS .app package, got %s", filepath.Base(req.FilePath)))
	}

	rep.Phase("auth")
	appID := s.configAppID
	if appID == "" {
		var err error
		appID, err = s.fetchAppID(ctx, req.PackageName)
		if err != nil {
			return "", fmt.Errorf("fetch app_id: %w", err)
		}
	}

	objectID, fileName, err := s.uploadPackage(ctx, appID, req.FilePath, rep)
	if err != nil {
		return "", fmt.Errorf("upload package: %w", err)
	}

	rep.Phase("binding package")
	packageID, err := s.bindPackage(ctx, appID, fileName, objectID)
	if err != nil {
		return "", fmt.Errorf("bind package: %w", err)
	}

	// Every step from here on has the package on Huawei's side; make the
	// recovery path explicit in each error.
	wrap := func(err error) error {
		return fmt.Errorf("%w (软件包已上传至 AGC，请到后台检查并完成提审：%s)", err, consoleURL)
	}

	if req.ReleaseNotes != "" {
		rep.Phase("release notes")
		if err := s.updateReleaseNotes(ctx, appID, req.ReleaseNotes); err != nil {
			return packageID, wrap(fmt.Errorf("update release notes: %w", err))
		}
	}

	rep.Phase("compiling")
	if err := s.waitCompiled(ctx, appID, packageID); err != nil {
		return packageID, wrap(err)
	}

	rep.Phase("submitting")
	if err := s.submitWithRetry(ctx, appID, req.ReleaseTime); err != nil {
		return packageID, wrap(err)
	}
	return packageID, nil
}

// fetchAppID resolves a bundleName to the AGC app ID, filtered to
// HarmonyOS package types so an Android app sharing the same name in the
// account is never picked by mistake.
func (s *Store) fetchAppID(ctx context.Context, bundleName string) (string, error) {
	var resp struct {
		Ret retInfo `json:"ret"`
		// AGC spells the key "appids" on this endpoint; encoding/json
		// matches case-insensitively so the Android-era "appIds" works too.
		AppIds []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"appids"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"packageName":  bundleName,
			"packageTypes": packageTypeHarmony,
		}).
		SetResult(&resp).
		Get("/api/publish/v2/appid-list")
	if err != nil {
		return "", err
	}
	if httpResp.IsError() {
		return "", httpError(httpResp)
	}
	if resp.Ret.Code != 0 {
		return "", store.Categorize(classify(resp.Ret), fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text()))
	}
	ids := resp.AppIds
	if len(ids) == 0 {
		return "", store.Categorize(store.CategoryConfigInvalid,
			fmt.Errorf("no HarmonyOS app found for bundleName %s in this AGC account (create it in AppGallery Connect first, or set app_id)", bundleName))
	}
	return ids[0].Value, nil
}

// uploadPackage runs the two-step Upload Management API: fetch a signed
// OBS PUT target for the file, then stream the bytes to it. Returns the
// objectId the package-info binding needs.
func (s *Store) uploadPackage(ctx context.Context, appID, path string, rep progress.Reporter) (objectID, fileName string, err error) {
	fileName = filepath.Base(path)
	size, sum, err := fileSizeAndSHA256(path)
	if err != nil {
		return "", "", err
	}

	info, err := s.getUploadURL(ctx, appID, fileName, size, sum)
	if err != nil {
		return "", "", err
	}

	rep.Phase("uploading")
	rc, _, err := progress.OpenFile(path, rep)
	if err != nil {
		return "", "", fmt.Errorf("open package: %w", err)
	}
	defer rc.Close()

	method := info.Method
	if method == "" {
		method = http.MethodPut
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, info.URL, rc)
	if err != nil {
		return "", "", err
	}
	httpReq.ContentLength = size
	httpReq.Header.Set("Content-Type", "application/octet-stream")
	for k, v := range info.Headers {
		// Host is a pseudo-header on the request struct, not a map entry.
		if strings.EqualFold(k, "Host") {
			httpReq.Host = v
			continue
		}
		httpReq.Header.Set(k, v)
	}
	resp, err := s.upload.Do(httpReq)
	if err != nil {
		return "", "", httpx.RedactURLError(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("put package: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return info.ObjectID, fileName, nil
}

// urlInfo is the AGC CommonUrlInfo payload.
type urlInfo struct {
	ObjectID string            `json:"objectId"`
	URL      string            `json:"url"`
	Method   string            `json:"method"`
	Headers  map[string]string `json:"headers"`
}

func (s *Store) getUploadURL(ctx context.Context, appID, fileName string, size int64, sha string) (*urlInfo, error) {
	var resp struct {
		Ret     retInfo `json:"ret"`
		URLInfo urlInfo `json:"urlInfo"`
	}
	q := map[string]string{
		"appId":         appID,
		"fileName":      fileName,
		"contentLength": strconv.FormatInt(size, 10),
	}
	if sha != "" {
		q["sha256"] = sha
	}
	if s.mainlandFlag != "" {
		q["chineseMainlandFlag"] = s.mainlandFlag
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(q).
		SetResult(&resp).
		Get("/api/publish/v2/upload-url/for-obs")
	if err != nil {
		return nil, err
	}
	if httpResp.IsError() {
		return nil, httpError(httpResp)
	}
	if resp.Ret.Code != 0 {
		return nil, store.Categorize(classify(resp.Ret), fmt.Errorf("upload-url: [%d] %s", resp.Ret.Code, resp.Ret.text()))
	}
	if resp.URLInfo.URL == "" || resp.URLInfo.ObjectID == "" {
		return nil, fmt.Errorf("upload-url: empty urlInfo in response")
	}
	return &resp.URLInfo, nil
}

// bindPackage attaches the uploaded object to the app's draft version
// and returns the packageId used to poll compile status.
func (s *Store) bindPackage(ctx context.Context, appID, fileName, objectID string) (string, error) {
	var resp struct {
		Ret       retInfo `json:"ret"`
		PackageID string  `json:"packageId"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{"appId": appID, "releaseType": "1"}).
		SetBody(map[string]any{"fileName": fileName, "objectId": objectID}).
		SetResult(&resp).
		Put("/api/publish/v3/app-package-info")
	if err != nil {
		return "", err
	}
	if httpResp.IsError() {
		return "", httpError(httpResp)
	}
	if resp.Ret.Code != 0 {
		return "", store.Categorize(classify(resp.Ret), fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text()))
	}
	return resp.PackageID, nil
}

// updateReleaseNotes writes newFeatures for one language. AGC requires
// the language code; when none is configured the app's default language
// is looked up so the notes land on the listing users actually see.
func (s *Store) updateReleaseNotes(ctx context.Context, appID, notes string) error {
	lang := s.lang
	if lang == "" {
		var err error
		if lang, err = s.defaultLang(ctx, appID); err != nil {
			return fmt.Errorf("resolve default language: %w", err)
		}
	}
	var resp struct {
		Ret retInfo `json:"ret"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{"appId": appID, "releaseType": "1"}).
		SetBody(map[string]any{"lang": lang, "newFeatures": notes}).
		SetResult(&resp).
		Put("/api/publish/v3/app-language-info")
	if err != nil {
		return err
	}
	if httpResp.IsError() {
		return httpError(httpResp)
	}
	if resp.Ret.Code != 0 {
		return fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text())
	}
	return nil
}

// appInfo is the subset of AGC's v3 AppInfo apkgo reads.
type appInfo struct {
	ReleaseState         int    `json:"releaseState"`
	DefaultLang          string `json:"defaultLang"`
	VersionNumber        string `json:"versionNumber"`
	VersionCode          int64  `json:"versionCode"`
	OnShelfVersionNumber string `json:"onShelfVersionNumber"`
	OnShelfVersionCode   int64  `json:"onShelfVersionCode"`
}

func (s *Store) getAppInfo(ctx context.Context, appID string) (*appInfo, error) {
	var resp struct {
		Ret     retInfo `json:"ret"`
		AppInfo appInfo `json:"appInfo"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{"appId": appID, "releaseType": "1"}).
		SetResult(&resp).
		Get("/api/publish/v3/app-info")
	if err != nil {
		return nil, err
	}
	if httpResp.IsError() {
		return nil, httpError(httpResp)
	}
	if resp.Ret.Code != 0 {
		return nil, store.Categorize(classify(resp.Ret), fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text()))
	}
	return &resp.AppInfo, nil
}

func (s *Store) defaultLang(ctx context.Context, appID string) (string, error) {
	ai, err := s.getAppInfo(ctx, appID)
	if err != nil {
		return "", err
	}
	if ai.DefaultLang == "" {
		return "zh-CN", nil
	}
	return ai.DefaultLang, nil
}

// waitCompiled polls the package compile status until AGC has parsed
// the package (successStatus 0). AGC documents ~2 minutes of async
// parsing after upload; a failed parse (2) is terminal and carries the
// reason code in the message (signature / certificate / bundleName
// mismatches, see the errorcode table), so it is surfaced as a policy
// block rather than retried.
func (s *Store) waitCompiled(ctx context.Context, appID, packageID string) error {
	if packageID == "" {
		return nil
	}
	deadline := time.Now().Add(s.compileTimeout)
	for {
		state, err := s.compileStatus(ctx, appID, packageID)
		if err != nil {
			return fmt.Errorf("compile status: %w", err)
		}
		switch state {
		case 0:
			return nil
		case 2:
			return store.Categorize(store.CategoryPolicyBlock,
				fmt.Errorf("AGC rejected the package while compiling (packageId %s); check signature, certificate and bundleName in the AGC console", packageID))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("package still compiling after %s (packageId %s)", s.compileTimeout, packageID)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("compile wait cancelled: %w", ctx.Err())
		case <-time.After(s.pollInterval):
		}
	}
}

func (s *Store) compileStatus(ctx context.Context, appID, packageID string) (int, error) {
	var resp struct {
		Ret          retInfo `json:"ret"`
		PkgStateList []struct {
			PkgID         string `json:"pkgId"`
			SuccessStatus int    `json:"successStatus"`
		} `json:"pkgStateList"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{"appId": appID, "pkgIds": packageID}).
		SetResult(&resp).
		Get("/api/publish/v3/package/compile/status")
	if err != nil {
		return 0, err
	}
	if httpResp.IsError() {
		return 0, httpError(httpResp)
	}
	if resp.Ret.Code != 0 {
		return 0, fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text())
	}
	for _, p := range resp.PkgStateList {
		if p.PkgID == packageID || len(resp.PkgStateList) == 1 {
			return p.SuccessStatus, nil
		}
	}
	// Unknown package: treat as still parsing rather than failing.
	return 1, nil
}

// submitWithRetry submits for review, retrying while AGC reports the
// package as still compiling (204144719 / 204144727 — "try again in 3–5
// minutes"). Any other non-zero ret is terminal.
func (s *Store) submitWithRetry(ctx context.Context, appID string, releaseTime *time.Time) error {
	deadline := time.Now().Add(s.compileTimeout)
	for {
		ret, err := s.submit(ctx, appID, releaseTime)
		if err != nil {
			return fmt.Errorf("submit: %w", err)
		}
		if ret.Code == 0 {
			return nil
		}
		if !isCompiling(ret) {
			return store.Categorize(classify(ret), fmt.Errorf("submit: [%d] %s", ret.Code, ret.text()))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("submit: package still compiling after %s: [%d] %s", s.compileTimeout, ret.Code, ret.text())
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("submit cancelled: %w", ctx.Err())
		case <-time.After(s.pollInterval):
		}
	}
}

func (s *Store) submit(ctx context.Context, appID string, releaseTime *time.Time) (retInfo, error) {
	var resp struct {
		Ret retInfo `json:"ret"`
	}
	body := map[string]any{"releaseType": 1}
	if releaseTime != nil {
		// AGC's releaseTime: "yyyy-MM-ddTHH:mm:ssZZ", e.g. 2026-06-20T10:00:00+0800.
		body["releaseTime"] = releaseTime.Format("2006-01-02T15:04:05Z0700")
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{"appId": appID}).
		SetBody(body).
		SetResult(&resp).
		Post("/api/publish/v3/app-submit")
	if err != nil {
		return retInfo{}, err
	}
	if httpResp.IsError() {
		return retInfo{}, httpError(httpResp)
	}
	return resp.Ret, nil
}

// isCompiling detects the transient "package still being processed"
// codes. 204144719 = 软件包正在编译, 204144727 = 编译中请 3–5 分钟后再试.
// 204144660 is a generic submit-failure code that Huawei also uses while
// parsing; match the message for that one.
func isCompiling(ret retInfo) bool {
	msg := strings.ToLower(ret.text())
	switch ret.Code {
	case 204144719, 204144727:
		return true
	case 204144660:
		return strings.Contains(msg, "pars") || strings.Contains(msg, "compil") || strings.Contains(msg, "解析") || strings.Contains(msg, "编译")
	}
	return false
}

// classify maps AGC ret codes to the unified category. Codes are reused
// across unrelated failures, so only the unambiguous ones are mapped.
func classify(ret retInfo) store.Category {
	switch ret.Code {
	case 204144665: // 鉴权失败、查询用户信息失败
		return store.CategoryAuthFailed
	case 204144720, 204144723: // 软件包编译失败 / 签名与其它版本不一致
		return store.CategoryPolicyBlock
	case 204144721, 204144756: // no app for packageTypes / 应用不存在
		return store.CategoryConfigInvalid
	case 204144664: // 提交审核前预检查失败 — console form incomplete
		return store.CategoryConfigInvalid
	case 204144659: // async queue full, retry later
		return store.CategoryStoreBusy
	}
	return store.CategoryUnknown
}

// audit is registered with `apkgo audit`: the v3 app-info releaseState
// carries the same state table as the Android endpoint.
func audit(ctx context.Context, cfg map[string]string, q store.AuditQuery) store.AuditResult {
	res := store.AuditResult{Store: StoreName}
	s, err := New(cfg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	appID := s.configAppID
	if appID == "" {
		if appID, err = s.fetchAppID(ctx, q.Package); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	ai, err := s.getAppInfo(ctx, appID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.State, res.Detail = mapReleaseState(ai.ReleaseState)
	res.VersionName = ai.VersionNumber
	res.VersionCode = int32(ai.VersionCode)
	res.LiveVersionName = ai.OnShelfVersionNumber
	res.LiveVersionCode = int32(ai.OnShelfVersionCode)
	return res
}

// mapReleaseState maps app-info releaseState (releaseType=1) to the
// unified state. 0=已上架, 1=上架审核不通过, 2=已下架, 3=待上架(预约), 4=审核中,
// 5=升级审核中, 6=申请下架, 7=草稿, 8=升级审核不通过, 9=下架审核不通过,
// 10=开发者下架, 11=撤销上架, 12=预审中, 13=预审不通过.
func mapReleaseState(state int) (store.AuditState, string) {
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

// diagnose is registered with `apkgo doctor`: credentials → bundleName
// resolves to a HarmonyOS appId → the account may issue upload URLs
// (which AGC only does with the App release role).
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 3)
	s, err := New(cfg)
	if err != nil {
		return append(probes, store.Probe{Name: "token", Status: "fail", Error: err.Error()})
	}
	probes = append(probes, store.Probe{Name: "token", Status: "ok", Detail: "auth mode: " + s.mode.String()})

	if hint.Package == "" {
		return append(probes,
			store.Probe{Name: "appid-list", Status: "skip", Detail: "needs --package or --file"},
			store.Probe{Name: "release-permission", Status: "skip", Detail: "needs --package or --file"},
		)
	}
	appID := s.configAppID
	if appID == "" {
		appID, err = s.fetchAppID(ctx, hint.Package)
		if err != nil {
			return append(probes,
				store.Probe{Name: "appid-list", Status: "fail", Error: err.Error()},
				store.Probe{Name: "release-permission", Status: "skip", Detail: "needs appid-list"},
			)
		}
		probes = append(probes, store.Probe{Name: "appid-list", Status: "ok", Detail: fmt.Sprintf("%s → %s (HarmonyOS)", hint.Package, appID)})
	} else {
		probes = append(probes, store.Probe{Name: "appid-list", Status: "skip", Detail: "using configured app_id=" + appID})
	}
	if _, err := s.getUploadURL(ctx, appID, "apkgo-doctor-probe.app", 1, ""); err != nil {
		return append(probes, store.Probe{Name: "release-permission", Status: "fail", Error: err.Error()})
	}
	return append(probes, store.Probe{Name: "release-permission", Status: "ok", Detail: "upload-url issued (App release permission granted)"})
}

func fileSizeAndSHA256(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func httpError(resp *resty.Response) error {
	msg := strings.TrimSpace(string(resp.Body()))
	if msg == "" {
		msg = resp.Status()
	}
	err := fmt.Errorf("http %d: %s", resp.StatusCode(), msg)
	switch resp.StatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return store.Categorize(store.CategoryAuthFailed, err)
	}
	if resp.StatusCode() >= 500 {
		return store.Categorize(store.CategoryNetworkRetry, err)
	}
	return err
}

// retInfo is AGC's standard response envelope; the message field is
// `msg` on most endpoints and `message` on a few.
type retInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

func (r retInfo) text() string {
	if r.Message != "" {
		return r.Message
	}
	return r.Msg
}
