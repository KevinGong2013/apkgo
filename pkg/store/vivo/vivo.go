package vivo

import (
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

	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

const vivoBaseURL = "https://developer-api.vivo.com.cn/router/rest"

func init() {
	store.Register("vivo", store.ConfigSchema{
		Name:                     "vivo",
		ConsoleURL:               "https://dev.vivo.com.cn/documentCenter/doc/326",
		SupportsScheduledRelease: true,
		SupportsURLPush:          true,
		Fields: []store.FieldSchema{
			{Key: "access_key", Required: true, Desc: "vivo open platform access key"},
			{Key: "access_secret", Required: true, Desc: "vivo open platform access secret"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("vivo", diagnose)
	store.RegisterAuditor("vivo", audit)
}

// audit is registered with `apkgo audit`. It reads the package's review
// status via the read-only app.query.details, independent of upload.
func audit(ctx context.Context, cfg map[string]string, q store.AuditQuery) store.AuditResult {
	res := store.AuditResult{Store: "vivo"}
	s, err := New(cfg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	app, err := s.queryApp(ctx, q.Package)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if app == nil {
		res.Error = fmt.Sprintf("no app found for package %s under this developer account", q.Package)
		return res
	}
	res.State, res.Detail = mapVivoAuditState(int(app.Status))
	res.VersionName = app.VersionName
	if vc, err := strconv.Atoi(strings.TrimSpace(app.VersionCode)); err == nil {
		res.VersionCode = int32(vc)
	}
	return res
}

// mapVivoAuditState maps vivo's app.query.details 审核状态 to the unified
// AuditState. 1=草稿, 2=待审核, 3=审核通过, 4=审核不通过, 5=撤销审核.
func mapVivoAuditState(status int) (store.AuditState, string) {
	switch status {
	case 2:
		return store.AuditReviewing, ""
	case 3:
		return store.AuditApproved, ""
	case 4:
		return store.AuditRejected, ""
	case 5:
		return store.AuditWithdrawn, ""
	case 1:
		return store.AuditUnknown, "draft (草稿) — not yet submitted"
	default:
		return store.AuditUnknown, fmt.Sprintf("status=%d", status)
	}
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
		SetBaseURL(vivoBaseURL)

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

func (s *Store) upload(ctx context.Context, req *store.UploadRequest) error {
	rep := progress.Safe(req.Progress)

	updateReq := map[string]string{
		"packageName":      req.PackageName,
		"versionCode":      strconv.Itoa(int(req.VersionCode)),
		"onlineType":       "1",
		"updateDesc":       req.ReleaseNotes,
		"compatibleDevice": "2",
	}
	if req.ReleaseTime != nil {
		// Scheduled release (定时上架): onlineType 2 = 定时上架, with
		// scheOnlineTime as a Beijing-local datetime string.
		updateReq["onlineType"] = "2"
		updateReq["scheOnlineTime"] = store.BeijingLocalTime(*req.ReleaseTime)
	}

	// URL pass-through (download mode): when -f (and --file64 for split)
	// are public URLs, hand vivo the download addresses and let it pull
	// the APKs itself (async), instead of uploading the bytes.
	if pushed, err := s.maybeURLPush(ctx, req, updateReq, rep); pushed {
		return err
	}

	// Pre-declare combined upload bytes so the bar is stable across
	// sequential 32/64-bit transfers.
	total, err := sumFileSizes(req.FilePath, req.File64Path)
	if err != nil {
		return fmt.Errorf("stat apk: %w", err)
	}
	rep.Phase("uploading")
	rep.Total(total)

	if req.File64Path != "" {
		// Split package upload
		resp32, err := s.uploadAPK("app.upload.apk.app.32", req.PackageName, req.FilePath, rep)
		if err != nil {
			return fmt.Errorf("upload 32-bit: %w", err)
		}
		resp64, err := s.uploadAPK("app.upload.apk.app.64", req.PackageName, req.File64Path, rep)
		if err != nil {
			return fmt.Errorf("upload 64-bit: %w", err)
		}
		updateReq["apk32"] = resp32.SerialNumber
		updateReq["apk64"] = resp64.SerialNumber
		rep.Phase("publishing")
		return s.updateApp("app.sync.update.subpackage.app", updateReq)
	}

	// Single package upload
	resp, err := s.uploadAPK("app.upload.apk.app", req.PackageName, req.FilePath, rep)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	updateReq["apk"] = resp.SerialNumber
	updateReq["fileMd5"] = resp.FileMD5
	rep.Phase("publishing")
	return s.updateApp("app.sync.update.app", updateReq)
}

// vivoTaskPollPeriod is how often we poll vivo's async task-status while it
// downloads the APK(s) from the developer URL. vivo documents no rate
// limit here, so a short interval is fine.
const vivoTaskPollPeriod = 15 * time.Second

// maybeURLPush implements vivo's download mode. When the APK(s) came in as
// public URLs it hands vivo the download addresses (the app.update.* async
// interfaces) and polls the task status, instead of uploading the bytes.
// Returns (true, err) when it owns the publish, or (false, nil) to fall
// through to the upload path (e.g. local file, or split with only one URL).
func (s *Store) maybeURLPush(ctx context.Context, req *store.UploadRequest, bizParams map[string]string, rep progress.Reporter) (bool, error) {
	switch {
	case req.File64Path != "":
		// Split arch: vivo's subpackage download needs BOTH public URLs;
		// if either side is a local file, fall back to the upload path.
		if req.SourceURL == "" || req.Source64URL == "" {
			return false, nil
		}
		md32, err := fileMD5(req.FilePath)
		if err != nil {
			return true, fmt.Errorf("md5 32-bit apk: %w", err)
		}
		md64, err := fileMD5(req.File64Path)
		if err != nil {
			return true, fmt.Errorf("md5 64-bit apk: %w", err)
		}
		// FilePath is the 32-bit (-f), File64Path the 64-bit (--file64);
		// vivo names the 64-bit md5 field apkMd5 and the 32-bit apk32Md5.
		bizParams["apkUrl32"] = req.SourceURL
		bizParams["apk32Md5"] = md32
		bizParams["apkUrl64"] = req.Source64URL
		bizParams["apkMd5"] = md64
		rep.Phase("url push")
		if err := s.updateApp("app.update.subpackage.app", bizParams); err != nil {
			return true, err
		}
	case req.SourceURL != "":
		md, err := fileMD5(req.FilePath)
		if err != nil {
			return true, fmt.Errorf("md5 apk: %w", err)
		}
		bizParams["apkUrl"] = req.SourceURL
		bizParams["apkMd5"] = md
		rep.Phase("url push")
		if err := s.updateApp("app.update.app", bizParams); err != nil {
			return true, err
		}
	default:
		return false, nil
	}

	// vivo processes the download asynchronously; poll until it finishes.
	rep.Phase("publishing")
	return true, s.pollTaskStatus(ctx, req.PackageName)
}

// pollTaskStatus polls app.query.task.status until vivo's async
// download-update resolves (status 3 = success, 4 = failure).
func (s *Store) pollTaskStatus(ctx context.Context, packageName string) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for vivo to download package: %w", ctx.Err())
		case <-time.After(vivoTaskPollPeriod):
		}
		status, reason, err := s.queryTaskStatus(packageName)
		if err != nil {
			return err
		}
		switch status {
		case 3: // 处理成功
			return nil
		case 4: // 处理失败
			if reason == "" {
				reason = "vivo reported the download task failed"
			}
			return store.Categorize(store.CategoryUnknown, fmt.Errorf("vivo download task failed: %s", reason))
		}
		// 1 待处理 / 2 处理中 → keep waiting
	}
}

// queryTaskStatus calls app.query.task.status for an update task
// (packetType 0) and returns the task status and any error reason.
func (s *Store) queryTaskStatus(packageName string) (status int, reason string, err error) {
	params := s.signParams("app.query.task.status", map[string]string{
		"packageName": packageName,
		"packetType":  "0", // 0 = update package
	})
	httpResp, err := s.client.R().SetQueryParams(params).Post("")
	if err != nil {
		return 0, "", err
	}
	body := httpResp.Body()
	var resp struct {
		envelope
		Data struct {
			Status      int    `json:"status"`
			ErrorReason string `json:"errorReason"`
		} `json:"data"`
	}
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return 0, "", fmt.Errorf("decode task status (HTTP %d): %v: %s",
			httpResp.StatusCode(), jerr, truncateBody(string(body)))
	}
	if resp.failed() {
		return 0, "", store.Categorize(classifyVivo(resp.SubCode),
			fmt.Errorf("[%s] %s", resp.errorCode(), resp.text()))
	}
	return resp.Data.Status, resp.Data.ErrorReason, nil
}

// sumFileSizes totals the byte sizes of the given paths. Empty paths are ignored.
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

func (s *Store) uploadAPK(method, packageName, filePath string, rep progress.Reporter) (*uploadResp, error) {
	fileMd5, err := fileMD5(filePath)
	if err != nil {
		return nil, err
	}

	params := s.signParams(method, map[string]string{
		"packageName": packageName,
		"fileMd5":     fileMd5,
	})

	rc, fSize, err := progress.WrapFile(filePath, rep)
	if err != nil {
		return nil, fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	queryVals := url.Values{}
	for k, v := range params {
		queryVals.Set(k, v)
	}
	httpResp, err := httpx.DoMultipart(context.Background(), httpx.MultipartRequest{
		Method: http.MethodPost,
		URL:    vivoBaseURL,
		Query:  queryVals,
		Files:  []httpx.FileField{{Field: "file", FileName: filepath.Base(filePath), Reader: rc, Size: fSize}},
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	body, _ := io.ReadAll(httpResp.Body)
	var resp struct {
		envelope
		Data *uploadResp `json:"data"`
	}
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return nil, fmt.Errorf("decode response (HTTP %d): %v: %s",
			httpResp.StatusCode, jerr, truncateBody(string(body)))
	}
	if resp.failed() {
		err := fmt.Errorf("[%s] %s (HTTP %d)",
			resp.errorCode(), resp.text(), httpResp.StatusCode)
		return nil, store.Categorize(classifyVivo(resp.SubCode), err)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("empty response data (HTTP %d): %s",
			httpResp.StatusCode, truncateBody(string(body)))
	}
	return resp.Data, nil
}

// classifyVivo maps known business-layer subCodes to apkgo's
// orchestrator-friendly Category enum. Codes not yet mapped fall
// through as CategoryUnknown — cloud should treat those as
// not-auto-retryable.
func classifyVivo(subCode string) store.Category {
	switch subCode {
	case "15042": // 请上传与历史签名一致的APK包
		return store.CategoryPolicyBlock
	case "11010": // 应用处理中，请勿重复提交
		return store.CategoryStoreBusy
	case "11011": // 开发者账号不存在该应用
		return store.CategoryConfigInvalid
	case "13002": // 包名属于其它开发者
		return store.CategoryConfigInvalid
	}
	return store.CategoryUnknown
}

// truncateBody caps a response body at 500 chars so diagnostic errors stay
// readable.
func truncateBody(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

func (s *Store) updateApp(method string, bizParams map[string]string) error {
	params := s.signParams(method, bizParams)

	httpResp, err := s.client.R().SetQueryParams(params).Post("")
	if err != nil {
		return err
	}
	body := httpResp.Body()
	var resp envelope
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return fmt.Errorf("decode response (HTTP %d): %v: %s",
			httpResp.StatusCode(), jerr, truncateBody(string(body)))
	}
	if resp.failed() {
		err := fmt.Errorf("[%s] %s (HTTP %d)",
			resp.errorCode(), resp.text(), httpResp.StatusCode())
		return store.Categorize(classifyVivo(resp.SubCode), err)
	}
	return nil
}

// envelope carries vivo's standard top-level error shape.
//
// vivo splits errors into two layers:
//   - Code: gateway-level result (0 = the call reached the service)
//   - SubCode: business-level error code; non-empty SubCode means the
//     call was accepted by the gateway but the service rejected
//     it (e.g. SubCode=15042 "请上传与历史签名一致的APK包").
//
// A response with Code=0 and a non-empty SubCode is therefore NOT a
// success — the original code only checking Code led to real upload
// failures being silently mapped to "empty response data".
//
// Different endpoints also use either `msg` or `message` for the human
// text; text() picks whichever is non-empty.
type envelope struct {
	Code    int    `json:"code"`
	SubCode string `json:"subCode,omitempty"`
	Msg     string `json:"msg,omitempty"`
	Message string `json:"message,omitempty"`
}

// failed reports whether the response should be treated as an error.
//
// vivo's success responses can carry SubCode in two forms depending on
// the endpoint: most return an empty SubCode, but some (notably the
// split-arch upload paths app.upload.apk.app.32 / .64) return the
// literal string "0". Treat both as success — only a non-empty,
// non-zero SubCode is a real business-layer error.
func (e envelope) failed() bool {
	return e.Code != 0 || (e.SubCode != "" && e.SubCode != "0")
}

func (e envelope) text() string {
	if e.Msg != "" {
		return e.Msg
	}
	return e.Message
}

// errorCode returns whichever code is most useful to print. SubCode
// (business-level) takes priority because Code is 0 in the
// gateway-OK-but-business-failed case.
func (e envelope) errorCode() string {
	if e.SubCode != "" {
		return e.SubCode
	}
	return strconv.Itoa(e.Code)
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

// lenientInt unmarshals from either a JSON number or a quoted string,
// and never fails the surrounding decode — vivo is inconsistent about
// whether numeric fields are quoted, and a strict int here would break
// the shared appDetails decode (and the doctor probe with it).
type lenientInt int

func (n *lenientInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	if v, err := strconv.Atoi(s); err == nil {
		*n = lenientInt(v)
	}
	return nil
}

// appDetails is the slice of fields apkgo needs from `app.query.details`.
// vivo returns more (app name in zh, online state, etc.) but we only
// surface what the doctor / audit paths report.
type appDetails struct {
	PackageName string     `json:"packageName"`
	AppName     string     `json:"appName"`
	VersionName string     `json:"versionName"`
	VersionCode string     `json:"versionCode"`
	Status      lenientInt `json:"status"` // 审核状态: 1草稿/2待审核/3通过/4不通过/5撤销
}

// queryApp calls the read-only `app.query.details` method. Used by the
// doctor probe; safe to invoke without side-effects.
//
// The body is unmarshalled by hand because vivo serves /router/rest
// with `Content-Type: text/plain;charset=utf-8` (yes, on a JSON body),
// and resty's auto-decode keys off content-type — so SetResult would
// silently leave the struct zero-valued, making a real auth failure
// look like "no app found".
func (s *Store) queryApp(ctx context.Context, packageName string) (*appDetails, error) {
	params := s.signParams("app.query.details", map[string]string{
		"packageName": packageName,
	})
	httpResp, err := s.client.R().SetContext(ctx).SetQueryParams(params).Post("")
	if err != nil {
		return nil, err
	}
	body := httpResp.Body()
	var resp struct {
		envelope
		Data *appDetails `json:"data"`
	}
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return nil, fmt.Errorf("decode response (HTTP %d): %v: %s",
			httpResp.StatusCode(), jerr, truncateBody(string(body)))
	}
	if resp.failed() {
		return nil, fmt.Errorf("[%s] %s (HTTP %d)",
			resp.errorCode(), resp.text(), httpResp.StatusCode())
	}
	return resp.Data, nil
}

// diagnose is registered with `apkgo doctor`. Single probe:
//
//	app-info — calls /router/rest with method=app.query.details, which
//	           both validates the HMAC-SHA256 signature server-side and
//	           checks that the package exists under this developer
//	           account. A package-name hint is required since vivo has
//	           no separate "verify credentials" endpoint to probe with
//	           an empty body.
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 1)

	s, err := New(cfg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "config", Status: "fail", Error: err.Error()})
		return probes
	}

	if hint.Package == "" {
		probes = append(probes, store.Probe{Name: "app-info", Status: "skip", Detail: "needs --package or --file (vivo has no auth-only endpoint)"})
		return probes
	}

	app, err := s.queryApp(ctx, hint.Package)
	if err != nil {
		probes = append(probes, store.Probe{Name: "app-info", Status: "fail", Error: err.Error()})
		return probes
	}
	if app == nil {
		probes = append(probes, store.Probe{Name: "app-info", Status: "fail", Error: fmt.Sprintf("no app found for package %s under this developer account", hint.Package)})
		return probes
	}
	detail := fmt.Sprintf("%s → %q versionCode=%s versionName=%s", app.PackageName, app.AppName, app.VersionCode, app.VersionName)
	probes = append(probes, store.Probe{Name: "app-info", Status: "ok", Detail: detail})
	return probes
}
