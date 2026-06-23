package tencent

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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v3/pkg/apk"
	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

// cosClient performs the pre-signed COS PUT. Deliberately no
// Client.Timeout — upload duration scales with APK size and uplink
// bandwidth, so the deadline must come from the caller's ctx instead
// of a one-size-fits-all cap.
var cosClient = &http.Client{}

func init() {
	store.Register("tencent", store.ConfigSchema{
		Name:                     "tencent",
		ConsoleURL:               "https://wikinew.open.qq.com/index.html#/iwiki/4015262492",
		SupportsScheduledRelease: true,
		Fields: []store.FieldSchema{
			{Key: "user_id", Required: true, Desc: "Tencent open platform developer user ID"},
			{Key: "access_secret", Required: true, Desc: "API access secret (账户管理 → API 发布接口 → 申请开通)"},
			{Key: "app_id", Required: false, Desc: "Tencent app ID (single-app fallback; required if app_id_map is empty)"},
			{Key: "app_id_map", Required: false, Desc: `JSON map of package_name → app_id for multi-app setups, e.g. '{"com.foo":"111","com.bar":"222"}'`},
			{Key: "package_name", Required: false, Desc: "Android package name (auto-detected from APK if omitted)"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("tencent", diagnose)
	store.RegisterAuditor("tencent", audit)
}

// tencentResp is the standard response envelope. Some endpoints use
// `message` instead of `msg` for the human text; text() prefers
// whichever is non-empty.
type tencentResp struct {
	Ret     int    `json:"ret"`
	Msg     string `json:"msg,omitempty"`
	Message string `json:"message,omitempty"`
}

func (r tencentResp) text() string {
	if r.Msg != "" {
		return r.Msg
	}
	return r.Message
}

const baseURL = "https://p.open.qq.com/open_file/developer_api"

type Store struct {
	client       *resty.Client
	userID       string
	accessSecret string
	appID        string            // single-app default; used when no app_id_map entry matches
	appIDMap     map[string]string // optional package_name → app_id mapping for multi-app setups
	packageName  string
}

func New(cfg map[string]string) (*Store, error) {
	userID := strings.TrimSpace(cfg["user_id"])
	accessSecret := strings.TrimSpace(cfg["access_secret"])
	appID := strings.TrimSpace(cfg["app_id"])
	appIDMapRaw := strings.TrimSpace(cfg["app_id_map"])
	packageName := strings.TrimSpace(cfg["package_name"]) // optional; falls back to APK metadata at upload time

	var appIDMap map[string]string
	if appIDMapRaw != "" {
		if err := json.Unmarshal([]byte(appIDMapRaw), &appIDMap); err != nil {
			return nil, fmt.Errorf("parse app_id_map: %w (expected JSON like '{\"com.foo\":\"111\",\"com.bar\":\"222\"}')", err)
		}
	}

	if userID == "" || accessSecret == "" {
		return nil, fmt.Errorf("user_id and access_secret are required")
	}
	if appID == "" && len(appIDMap) == 0 {
		return nil, fmt.Errorf("either app_id or app_id_map must be set (Tencent has no listing API, so app_id can't be auto-discovered)")
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
		appIDMap:     appIDMap,
		packageName:  packageName,
	}, nil
}

// resolveAppID returns the app_id to use for a given package. The map
// takes precedence over the single-app default — that way a mistakenly
// inherited `app_id` from a one-app setup can't silently cause a
// multi-app upload to push to the wrong app.
func (s *Store) resolveAppID(pkg string) (string, error) {
	if id, ok := s.appIDMap[pkg]; ok && id != "" {
		return id, nil
	}
	if s.appID != "" {
		return s.appID, nil
	}
	return "", fmt.Errorf("no app_id configured for package %q (not in app_id_map and no single-app fallback)", pkg)
}

// resolvePackage returns the configured package_name, falling back to
// the package name parsed from the APK. Tencent's APIs all require
// pkg_name as a sign param, but apkgo already extracts it from the
// APK itself, so requiring the user to repeat it in config is just
// duplicate state that can drift.
func (s *Store) resolvePackage(fallback string) (string, error) {
	if s.packageName != "" {
		return s.packageName, nil
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("package_name is empty and no APK package available")
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
	rep := progress.Safe(req.Progress)

	pkg, err := s.resolvePackage(req.PackageName)
	if err != nil {
		return err
	}
	appID, err := s.resolveAppID(pkg)
	if err != nil {
		return err
	}

	// Pre-declare combined upload bytes for a stable progress bar across
	// sequential apk + apk64 transfers.
	total, err := sumFileSizes(req.FilePath, req.File64Path)
	if err != nil {
		return fmt.Errorf("stat apk: %w", err)
	}
	rep.Phase("uploading")
	rep.Total(total)

	// 1. Upload APK file → get serial number
	apkSerial, apkMD5, err := s.uploadFile(ctx, pkg, appID, req.FilePath, "apk", rep)
	if err != nil {
		return fmt.Errorf("upload apk: %w", err)
	}

	// 2. Upload 64-bit APK if provided
	var apk64Serial, apk64MD5 string
	if req.File64Path != "" {
		apk64Serial, apk64MD5, err = s.uploadFile(ctx, pkg, appID, req.File64Path, "apk", rep)
		if err != nil {
			return fmt.Errorf("upload 64-bit apk: %w", err)
		}
	}

	// 3. Submit update. The app is now submitted and under review (审核中).
	// Review progress is decoupled from upload — poll it with `apkgo audit`
	// (which runs on its own context) instead of blocking the upload here.
	rep.Phase("publishing")
	return s.updateApp(pkg, appID, req, apkSerial, apkMD5, apk64Serial, apk64MD5)
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

// uploadFile gets a pre-signed COS URL, streams the file, and returns the
// serial number and file MD5. Reads the file in a streaming fashion so
// memory use stays bounded regardless of APK size, and reports byte-level
// progress to rep via a progress.Reader wrapper.
func (s *Store) uploadFile(ctx context.Context, pkg, appID, filePath, fileType string, rep progress.Reporter) (serialNumber string, fileMd5 string, err error) {
	fileName := filepath.Base(filePath)

	// Get upload info
	params := url.Values{}
	params.Set("pkg_name", pkg)
	params.Set("app_id", appID)
	params.Set("file_type", fileType)
	params.Set("file_name", fileName)

	var resp struct {
		tencentResp
		PreSignURL   string `json:"pre_sign_url"`
		SerialNumber string `json:"serial_number"`
	}
	if err := s.post("/get_file_upload_info", params, &resp); err != nil {
		return "", "", err
	}
	if resp.Ret != 0 {
		return "", "", fmt.Errorf("[%d] %s", resp.Ret, resp.text())
	}
	if resp.PreSignURL == "" || resp.SerialNumber == "" {
		return "", "", fmt.Errorf("get_file_upload_info: empty pre_sign_url or serial_number (ret=%d msg=%q)", resp.Ret, resp.text())
	}

	// Calculate MD5 (streaming, no buffering of the whole file)
	fileMd5, err = calcFileMD5(filePath)
	if err != nil {
		return "", "", fmt.Errorf("calc md5: %w", err)
	}

	// Open the file once and stream it to COS. Set ContentLength so the
	// HTTP client sends a real Content-Length header instead of falling
	// back to chunked encoding, which COS may reject.
	f, err := os.Open(filePath)
	if err != nil {
		return "", "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", "", fmt.Errorf("stat file: %w", err)
	}

	body := &progress.Reader{R: f, Reporter: progress.Safe(rep)}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, resp.PreSignURL, body)
	if err != nil {
		return "", "", fmt.Errorf("create put request: %w", err)
	}
	httpReq.ContentLength = fi.Size()
	httpReq.Header.Set("Content-Type", "application/octet-stream")

	// No Client.Timeout here: a flat per-request cap kills legitimate
	// large-APK uploads on slow uplinks (a 60MB APK over a ~200KB/s line
	// needs >5 min). Cancellation comes from ctx, which the CLI bounds
	// with the job timeout and apkgo-cloud bounds with its per-store cap.
	httpResp, err := cosClient.Do(httpReq)
	if err != nil {
		return "", "", fmt.Errorf("upload to cos: %w", httpx.RedactURLError(err))
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return "", "", fmt.Errorf("cos upload failed: HTTP %d: %s", httpResp.StatusCode, string(body))
	}

	return resp.SerialNumber, fileMd5, nil
}

// updateApp submits the app update with APK serial numbers and metadata.
//
// Tencent's flags use 1=是(upload), 2=否(don't) — there is NO 0; sending
// apk32_flag=0 is rejected with `[4000049] 无效的请求参数apk32_flag`. Valid
// single-/split-file combinations:
//   - apk32_flag=1                — 32-bit or 32&64 compatible package
//   - apk32_flag=2, apk64_flag=1  — 64-bit-only package
//   - apk32_flag=1, apk64_flag=1  — split-arch (separate 32 + 64 APKs)
//
// Submitting an arm64-only APK as `apk32_flag=1` fails server-side with
// `[4000045] 解析校验32位或32&64位兼容包失败`, so when no --file64 is given
// we inspect lib/<abi>/ to pick the right single-file branch.
func (s *Store) updateApp(pkg, appID string, req *store.UploadRequest, apkSerial, apkMD5, apk64Serial, apk64MD5 string) error {
	params := url.Values{}
	params.Set("pkg_name", pkg)
	params.Set("app_id", appID)
	params.Set("deploy_type", "1") // publish immediately after approval
	if req.ReleaseTime != nil {
		// Scheduled release (定时发布): deploy_type 2 = 定时发布, with
		// deploy_time as a Unix *second* timestamp (absolute instant;
		// Tencent documents it as Beijing time but epoch seconds are
		// timezone-independent).
		params.Set("deploy_type", "2")
		params.Set("deploy_time", strconv.FormatInt(req.ReleaseTime.Unix(), 10))
	}

	// APK files
	switch {
	case apk64Serial != "":
		// Split arch: 32-bit + 64-bit
		params.Set("apk32_flag", "1")
		params.Set("apk32_file_serial_number", apkSerial)
		params.Set("apk32_file_md5", apkMD5)
		params.Set("apk64_flag", "1")
		params.Set("apk64_file_serial_number", apk64Serial)
		params.Set("apk64_file_md5", apk64MD5)
	case isAPK64BitOnly(req.FilePath):
		// Single 64-bit-only APK. apk32_flag=2 (否) — NOT 0, which Tencent
		// rejects as 无效的请求参数.
		params.Set("apk32_flag", "2")
		params.Set("apk64_flag", "1")
		params.Set("apk64_file_serial_number", apkSerial)
		params.Set("apk64_file_md5", apkMD5)
	default:
		// Single APK (32&64 compatible, or universal/no native libs)
		params.Set("apk32_flag", "1")
		params.Set("apk32_file_serial_number", apkSerial)
		params.Set("apk32_file_md5", apkMD5)
	}

	// Release notes
	if req.ReleaseNotes != "" {
		params.Set("feature", req.ReleaseNotes)
	}

	var resp tencentResp
	if err := s.post("/update_app", params, &resp); err != nil {
		return err
	}
	if resp.Ret != 0 {
		return fmt.Errorf("[%d] %s", resp.Ret, resp.text())
	}
	return nil
}

// isAPK64BitOnly reports whether the APK at path contains only 64-bit
// native libs. Failures are swallowed and reported as false — the APK
// already uploaded successfully, so the worst case is that we fall back
// to the legacy apk32_flag=1 branch, matching prior behavior.
func isAPK64BitOnly(path string) bool {
	abis, err := apk.ABIs(path)
	if err != nil {
		return false
	}
	return apk.Is64BitOnly(abis)
}

// audit is registered with `apkgo audit`. It looks up the latest
// submission's review status for the package, independent of the upload
// flow — it builds its own client from the raw config so it can run on an
// independent context (e.g. a watch loop or a cron).
func audit(ctx context.Context, cfg map[string]string, q store.AuditQuery) store.AuditResult {
	res := store.AuditResult{Store: "tencent"}
	s, err := New(cfg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	pkg, err := s.resolvePackage(q.Package)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	appID, err := s.resolveAppID(pkg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	state, detail, err := s.queryAuditStatus(pkg, appID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.State = state
	res.Detail = detail
	// The open-API audit endpoint returns no version, so best-effort scrape
	// the public 应用宝 detail page for the currently-live version name. Any
	// failure leaves LiveVersionName empty — it never fails the audit.
	if live, ok := liveVersionFromStorePage(ctx, pkg); ok {
		res.LiveVersionName = live
	}
	return res
}

// storePageBaseURL is the public (unauthenticated) 应用宝 web detail page,
// keyed by package name. Unlike the signed open API it carries the live
// version, embedded in a Next.js __NEXT_DATA__ JSON blob.
const storePageBaseURL = "https://sj.qq.com/appdetail/"

// storePageClient fetches the public detail page. Separate from the signed
// open-API resty client: no auth, its own short timeout (the page is ~300 KB
// and this is a best-effort enrichment, not on the critical path).
var storePageClient = &http.Client{Timeout: 15 * time.Second}

var nextDataRe = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__"[^>]*>(.*?)</script>`)

// liveVersionFromStorePage best-effort extracts the live version_name for pkg
// from the public 应用宝 detail page. Returns ("", false) on any failure
// (network, non-200, markup change, package absent) so the caller can leave
// LiveVersionName empty rather than surfacing an error — the audit state from
// the open API is authoritative and must not be lost to a scrape hiccup.
func liveVersionFromStorePage(ctx context.Context, pkg string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, storePageBaseURL+url.PathEscape(pkg), nil)
	if err != nil {
		return "", false
	}
	// A real-browser UA: the page is server-rendered and a blank UA can be served a stub.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	httpResp, err := storePageClient.Do(req)
	if err != nil {
		return "", false
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 8<<20)) // cap at 8 MiB
	if err != nil {
		return "", false
	}
	m := nextDataRe.FindSubmatch(body)
	if m == nil {
		return "", false
	}
	var data any
	if err := json.Unmarshal(m[1], &data); err != nil {
		return "", false
	}
	if v := findVersionName(data, pkg); v != "" {
		return v, true
	}
	return "", false
}

// findVersionName walks the decoded __NEXT_DATA__ tree for the object whose
// pkg_name matches pkg and returns its version_name. The page embeds many app
// records (recommendations, similar apps); only the one for pkg carries the
// version we want, and a stub reference with an empty version_name is skipped.
func findVersionName(node any, pkg string) string {
	switch n := node.(type) {
	case map[string]any:
		if p, _ := n["pkg_name"].(string); p == pkg {
			if v, _ := n["version_name"].(string); v != "" {
				return v
			}
		}
		for _, v := range n {
			if got := findVersionName(v, pkg); got != "" {
				return got
			}
		}
	case []any:
		for _, v := range n {
			if got := findVersionName(v, pkg); got != "" {
				return got
			}
		}
	}
	return ""
}

// queryAuditStatus does a single audit-status query and maps Tencent's
// audit_status to the unified AuditState (1=审核中, 2=驳回, 3=通过, 8=撤回).
func (s *Store) queryAuditStatus(pkg, appID string) (store.AuditState, string, error) {
	params := url.Values{}
	params.Set("pkg_name", pkg)
	params.Set("app_id", appID)

	var resp struct {
		tencentResp
		AuditStatus int    `json:"audit_status"`
		AuditReason string `json:"audit_reason"`
	}
	if err := s.post("/query_app_update_status", params, &resp); err != nil {
		return "", "", err
	}
	if resp.Ret != 0 {
		return "", "", fmt.Errorf("[%d] %s", resp.Ret, resp.text())
	}
	switch resp.AuditStatus {
	case 1:
		return store.AuditReviewing, "", nil
	case 2:
		return store.AuditRejected, resp.AuditReason, nil
	case 3:
		return store.AuditApproved, "", nil
	case 8:
		return store.AuditWithdrawn, "", nil
	default:
		return store.AuditUnknown, fmt.Sprintf("audit_status=%d", resp.AuditStatus), nil
	}
}

// post makes a signed POST request to the Tencent API.
//
// HTTP status is checked before attempting JSON decode so a 4xx/5xx
// with a non-JSON body (gateway HTML, empty, etc.) surfaces verbatim
// instead of being silently mapped to a zero-valued result struct
// (which would look like ret=0 success).
func (s *Store) post(path string, params url.Values, result any) error {
	// Add common params
	params.Set("user_id", s.userID)
	params.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))

	// Calculate sign
	params.Set("sign", s.calcSign(params))

	httpResp, err := s.client.R().
		SetBody(params.Encode()).
		Post(path)
	if err != nil {
		return err
	}
	body := httpResp.Body()
	if httpResp.IsError() {
		return fmt.Errorf("http %d: %s", httpResp.StatusCode(), truncateBody(string(body)))
	}
	if jerr := json.Unmarshal(body, result); jerr != nil {
		return fmt.Errorf("decode response (HTTP %d): %v: %s",
			httpResp.StatusCode(), jerr, truncateBody(string(body)))
	}
	return nil
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

// diagnose is registered with `apkgo doctor`. Two probes:
//
//	app-detail   — calls /query_app_detail to verify HMAC-SHA256 sign +
//	               auth path AND that the app_id/pkg_name combo binds
//	               correctly under this developer (ret 1000009 if not).
//	               Reports app_name + category for sanity.
//	audit-status — calls /query_app_update_status to surface the most
//	               recent submission's audit state (auditing / approved /
//	               rejected / withdrawn) so the operator knows whether
//	               the slot is free for a new upload.
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 2)

	s, err := New(cfg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "config", Status: "fail", Error: err.Error()})
		return probes
	}

	pkg, err := s.resolvePackage(hint.Package)
	if err != nil {
		probes = append(probes, store.Probe{Name: "app-detail", Status: "skip",
			Detail: "needs --package, --file, or package_name in config"})
		return probes
	}
	appID, err := s.resolveAppID(pkg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "app-detail", Status: "fail", Error: err.Error()})
		return probes
	}

	// Probe 1: app-detail
	var detailResp struct {
		tencentResp
		AppName  string `json:"app_name"`
		Category int    `json:"category"`
		Feature  string `json:"feature"`
	}
	params := url.Values{}
	params.Set("pkg_name", pkg)
	params.Set("app_id", appID)
	if err := s.post("/query_app_detail", params, &detailResp); err != nil {
		probes = append(probes, store.Probe{Name: "app-detail", Status: "fail", Error: err.Error()})
		return probes
	}
	if detailResp.Ret != 0 {
		probes = append(probes, store.Probe{Name: "app-detail", Status: "fail",
			Error: fmt.Sprintf("[%d] %s", detailResp.Ret, detailResp.text())})
		return probes
	}
	probes = append(probes, store.Probe{Name: "app-detail", Status: "ok",
		Detail: fmt.Sprintf("%s (app_id=%s) → app_name=%q category=%d", pkg, appID, detailResp.AppName, detailResp.Category)})

	// Probe 2: audit-status
	var auditResp struct {
		tencentResp
		AuditStatus int    `json:"audit_status"`
		AuditReason string `json:"audit_reason"`
	}
	if err := s.post("/query_app_update_status", params, &auditResp); err != nil {
		probes = append(probes, store.Probe{Name: "audit-status", Status: "fail", Error: err.Error()})
		return probes
	}
	if auditResp.Ret != 0 {
		probes = append(probes, store.Probe{Name: "audit-status", Status: "fail",
			Error: fmt.Sprintf("[%d] %s", auditResp.Ret, auditResp.text())})
		return probes
	}
	auditDetail := fmt.Sprintf("audit_status=%d (%s)", auditResp.AuditStatus, auditStatusName(auditResp.AuditStatus))
	if auditResp.AuditReason != "" {
		auditDetail += " reason=" + auditResp.AuditReason
	}
	probes = append(probes, store.Probe{Name: "audit-status", Status: "ok", Detail: auditDetail})
	return probes
}

// auditStatusName labels Tencent's audit_status integer values for readability.
func auditStatusName(s int) string {
	switch s {
	case 0:
		return "no submission"
	case 1:
		return "auditing"
	case 2:
		return "rejected"
	case 3:
		return "approved"
	case 8:
		return "withdrawn"
	default:
		return "unknown"
	}
}
