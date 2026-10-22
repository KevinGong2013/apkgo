package oppo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/ctxlog"
	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
	"github.com/KevinGong2013/apkgo/v4/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

func init() {
	store.Register("oppo", store.ConfigSchema{
		Name:                     "oppo",
		ConsoleURL:               "https://open.oppomobile.com/new/developmentDoc/info?id=10998",
		SupportsScheduledRelease: true,
		Fields: []store.FieldSchema{
			{Key: "client_id", Required: true, Desc: "OPPO open platform client ID"},
			{Key: "client_secret", Required: true, Desc: "OPPO open platform client secret"},
		},
		Listing: listingSpec,
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("oppo", diagnose)
	store.RegisterAuditor("oppo", audit)
	store.RegisterListingFetcher("oppo", fetchListing)
}

// audit is registered with `apkgo audit`. It reads the latest version's
// review status from the read-only app/info query, independent of upload.
func audit(ctx context.Context, cfg map[string]string, q store.AuditQuery) store.AuditResult {
	res := store.AuditResult{Store: "oppo"}
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
	res.State, res.Detail = mapOppoAudit(app.AuditStatusName, app.RefuseReason)
	res.VersionName = app.VersionName
	if vc, err := strconv.ParseInt(strings.TrimSpace(app.VersionCode), 10, 32); err == nil {
		res.VersionCode = int32(vc)
	}
	return res
}

// mapOppoAudit maps OPPO's audit_status_name (the human label from
// app/info) to the unified state. OPPO returns a numeric audit_status plus
// this description; we key off the description's keywords since the code
// table isn't published in the API传包 docs. The exact label is always
// surfaced in Detail so the operator sees ground truth regardless.
func mapOppoAudit(name, refuse string) (store.AuditState, string) {
	switch {
	case containsAny(name, "拒绝", "不通过", "驳回", "失败", "打回"):
		// A submission the developer withdrew in the console comes back as
		// "审核不通过" with the reason "开发者申请撤销审核" — not a rejection.
		if containsAny(refuse, "撤销审核", "撤回审核") {
			return store.AuditWithdrawn, name + ": " + refuse
		}
		if refuse != "" {
			return store.AuditRejected, name + ": " + refuse
		}
		return store.AuditRejected, name
	case containsAny(name, "上线", "上架", "已发布", "通过"):
		return store.AuditApproved, name
	case containsAny(name, "下架", "撤销", "冻结"):
		return store.AuditWithdrawn, name
	case containsAny(name, "审核", "待审", "审查", "送审"):
		return store.AuditReviewing, name
	case name == "":
		return store.AuditUnknown, "no audit status returned"
	default:
		return store.AuditUnknown, name
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
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
		Message string          `json:"message,omitempty"`
		Msg     string          `json:"msg,omitempty"`
		Data    json.RawMessage `json:"data,omitempty"`
	}
	_ = json.Unmarshal(body, &probe)
	for _, candidate := range []string{probe.Message, probe.Msg} {
		if candidate != "" {
			return candidate
		}
	}
	if len(probe.Data) > 0 {
		var dataProbe struct {
			Message string `json:"message,omitempty"`
			Msg     string `json:"msg,omitempty"`
		}
		if err := json.Unmarshal(probe.Data, &dataProbe); err == nil {
			for _, candidate := range []string{dataProbe.Message, dataProbe.Msg} {
				if candidate != "" {
					return candidate
				}
			}
		}
	}
	return strings.TrimSpace(string(body))
}

type Store struct {
	client       *resty.Client
	uploadClient *http.Client // streamed file uploads; nil = httpx's default
	clientID     string
	clientSecret string

	mu          sync.Mutex // guards accessToken, replaced by refreshToken
	accessToken string
}

func New(cfg map[string]string) (*Store, error) {
	clientID := strings.TrimSpace(cfg["client_id"])
	clientSecret := strings.TrimSpace(cfg["client_secret"])
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required")
	}

	client := newAPIClient("https://oop-openapi-cn.heytapmobi.com")
	trace := httptrace.ForStore("oppo", cfg)
	trace.Client(client.GetClient())

	token, err := fetchToken(client, clientID, clientSecret)
	if err != nil {
		return nil, store.AuthError(fmt.Errorf("auth: %w", err))
	}

	return &Store{
		client:       client,
		uploadClient: trace.NewClient(30 * time.Minute),
		accessToken:  token,
		clientID:     clientID,
		clientSecret: clientSecret,
	}, nil
}

// errnoTokenInvalid is OPPO's "[800003] Access Token无效".
//
// On 2026-10-21 an upload's token, issued at 16:25 with expire_in two days
// out, was refused at 16:30 while the accepted publish's task-state was
// being polled: the version had gone in, the upload was reported failed.
// Nothing in that run asked for another token; the same key used
// elsewhere at the same time (the developer's own scripts, another tool)
// is the likely cause. A fresh token from the same key fixes it.
const errnoTokenInvalid = 800003

// call sends a signed request — send signs it each time it runs — and,
// when OPPO answers 800003, fetches a new token and sends it once more.
// OPPO refuses such a request before acting on it, so repeating it, the
// submit (/app/upd) included, can't apply anything twice. If the new
// token can't be had, the 800003 answer is returned as it was.
func (s *Store) call(ctx context.Context, send func() (*resty.Response, error)) (*resty.Response, error) {
	resp, err := send()
	if err != nil || !tokenInvalid(resp) {
		return resp, err
	}
	if rerr := s.refreshToken(); rerr != nil {
		ctxlog.FromContext(ctx).Warn("oppo access token refused; fetching a new one failed", "error", rerr)
		return resp, err
	}
	ctxlog.FromContext(ctx).Info("oppo access token refused (800003); resending with a new one", "path", resp.RawResponse.Request.URL.Path)
	return send()
}

func tokenInvalid(resp *resty.Response) bool {
	var env errEnvelope
	return json.Unmarshal(resp.Body(), &env) == nil && env.Errno == errnoTokenInvalid
}

// refreshToken replaces the store's access token with a newly issued one.
func (s *Store) refreshToken() error {
	token, err := fetchToken(s.client, s.clientID, s.clientSecret)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.accessToken = token
	s.mu.Unlock()
	return nil
}

// apiRetryWait is the first pause before re-sending an idempotent call;
// tests shorten it.
var apiRetryWait = time.Second

// newAPIClient is the client for OPPO's open API. OPPO's gateway now and
// then answers 504 NoStackTraceTimeoutException after ~3 s, and
// connections drop; calls that are safe to repeat are re-sent twice
// before giving up (see retryIdempotent).
func newAPIClient(baseURL string) *resty.Client {
	return resty.New().
		SetBaseURL(baseURL).
		SetHeader("Content-Type", "application/json").
		SetRetryCount(2).
		SetRetryWaitTime(apiRetryWait).
		SetRetryMaxWaitTime(3 * apiRetryWait).
		AddRetryCondition(retryIdempotent)
}

// retryIdempotent re-sends a call that failed in transit or with a 5xx
// only when repeating it is harmless: every GET (token, app info,
// get-upload-url — each fetches a fresh one-time sign) and the task-state
// query. The submit (POST app/upd) is never repeated, so a slow but
// successful submit can't go in twice. With a condition set, resty
// retries exactly when it says so.
func retryIdempotent(r *resty.Response, err error) bool {
	if r == nil || r.Request == nil {
		return false
	}
	idempotent := r.Request.Method == http.MethodGet ||
		strings.HasSuffix(r.Request.URL, "/resource/v1/app/task-state")
	return idempotent && (err != nil || r.StatusCode() >= 500)
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
		return "", store.StatusError("", httpResp.StatusCode(), string(httpResp.Body()))
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
		if store.IsAlreadyDone(err) {
			return store.NewResultC(s.Name(), start, store.CategoryAlreadyDone)
		}
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(ctx context.Context, req *store.UploadRequest) error {
	rep := progress.Safe(req.Progress)

	// 1. Query app info
	rep.Phase("query")
	app, err := s.queryApp(ctx, req.PackageName)
	if err != nil {
		return fmt.Errorf("query app: %w", err)
	}
	if app == nil {
		return fmt.Errorf("query app: 未在 OPPO 开放平台该开发者账号下找到应用 %q（OPPO 开放接口仅支持版本更新，请先在 OPPO 开发者后台创建该应用并首发通过，或检查凭证是否匹配）", req.PackageName)
	}

	// Pre-declare the combined upload size so the bar is stable across
	// apk and apk64 transfers.
	apkFile, apk64File := req.Package(), req.Package64()
	totalBytes, err := store.TotalSize(apkFile, apk64File)
	if err != nil {
		return fmt.Errorf("stat apk: %w", err)
	}
	rep.Phase("uploading")
	rep.Total(totalBytes)

	// 2. Upload APK
	uploadResult, err := s.uploadAPK(ctx, apkFile, rep)
	if err != nil {
		return fmt.Errorf("upload apk: %w", err)
	}

	apkInfos := []apkInfo{{URL: uploadResult.URL, MD5: uploadResult.MD5, CpuCode: 0}}

	if req.File64Path != "" {
		apkInfos[0].CpuCode = 32
		result64, err := s.uploadAPK(ctx, apk64File, rep)
		if err != nil {
			return fmt.Errorf("upload 64-bit apk: %w", err)
		}
		apkInfos = append(apkInfos, apkInfo{URL: result64.URL, MD5: result64.MD5, CpuCode: 64})
	}

	// 3. Listing (商店资料): upload its images and swap the new values into
	// the fields /app/upd round-trips, so they're reviewed with this
	// version. Any failure stops here, before anything is submitted.
	if !req.Listing.Empty() {
		rep.Phase("listing")
		if err := s.applyListing(ctx, app, req.Listing); err != nil {
			return fmt.Errorf("listing: %w", err)
		}
	}

	// OPPO's /app/upd re-validates the icon_url it round-trips from /app/info
	// (must be a 512×512 PNG <1MB). If the stored icon doesn't comply, swap in
	// one extracted from the APK and re-uploaded; otherwise this is a no-op.
	// A listing icon was already validated against the same rule, so it
	// replaces the stored one as is.
	if req.Listing == nil || req.Listing.Icon == "" {
		app.IconURL = s.compliantIconURL(ctx, app.IconURL, apkFile, rep)
	}

	// 4. Publish. Two errno cases here are not real failures:
	//
	//   911216 "版本更新任务处理中" — a previous publish for this same
	//     version is still being processed. Skip ahead to polling; the
	//     existing task will reach terminal state on its own.
	//
	//   911215 "应用审核中" — the previous task has already finished and
	//     the version is in OPPO's review queue. apkgo's job is done;
	//     return success.
	//
	// In both cases OPPO kept the earlier submission, so this run's listing
	// never reached it: the result is already-done, which keeps
	// store.ListingResult from claiming the listing.
	rep.Phase("publishing")
	inFlight := false
	if err := s.publish(ctx, req, app, apkInfos); err != nil {
		switch {
		case isOppoUnderReview(err):
			return &store.AlreadyDoneError{Reason: "version already in OPPO review queue"}
		case isOppoTaskInFlight(err):
			inFlight = true // fall through to polling
		default:
			return fmt.Errorf("publish: %w", s.withAuditReason(ctx, req.PackageName, err))
		}
	}

	// 5. Poll task state
	rep.Phase("polling")
	if err := s.pollTaskState(ctx, req.PackageName, strconv.Itoa(int(req.VersionCode))); err != nil {
		return err
	}
	if inFlight && !req.Listing.Empty() {
		return &store.AlreadyDoneError{Reason: "an earlier publish of this version was still in progress; this run's listing was not submitted"}
	}
	return nil
}

// isOppoTaskInFlight reports whether the publish failure is the
// "previous task still in progress" case (errno 911216), where the
// correct action is to wait for the existing task rather than create
// a new one.
func isOppoTaskInFlight(err error) bool {
	return err != nil && strings.Contains(err.Error(), "[911216]")
}

// isOppoUnderReview reports whether the publish failure means the
// version has already been accepted into OPPO's review queue (errno
// 911215). From apkgo's perspective this is the success terminal
// state — the upload + publish pipeline is complete and the rest is
// up to OPPO's reviewers.
func isOppoUnderReview(err error) bool {
	return err != nil && strings.Contains(err.Error(), "[911215]")
}

// apiError is a non-zero errno from OPPO's open API. Its text is the
// "[errno] message" the store has always reported, which callers and
// apkgo cloud's hints match on.
type apiError struct {
	Errno int
	Msg   string
}

func (e *apiError) Error() string { return fmt.Sprintf("[%d] %s", e.Errno, e.Msg) }

// auditLookupTimeout bounds withAuditReason's app/info query, so a slow
// gateway can't hold up the publish error.
const auditLookupTimeout = 15 * time.Second

// withAuditReason adds OPPO's review verdict to a 911xxx publish error.
// /app/upd's message for those can name a different cause than the real
// one: on 2026-10-16 it answered "[911001] 版权证书无效" for an app OPPO
// had rejected as a 马甲 of another app (APK 相似度 0.92), a reason only
// app/info's refuse_reason gave. So when app/info shows the latest
// version rejected, its status and reason are appended. 800xxx errors
// (bad fields, token) are left alone: an earlier rejection says nothing
// about them. If the query fails, err is returned as it was.
func (s *Store) withAuditReason(ctx context.Context, pkgName string, err error) error {
	var ae *apiError
	if !errors.As(err, &ae) || ae.Errno/1000 != 911 {
		return err
	}
	qctx, cancel := context.WithTimeout(ctx, auditLookupTimeout)
	defer cancel()
	app, qerr := s.queryApp(qctx, pkgName)
	if qerr != nil {
		ctxlog.FromContext(ctx).Warn("oppo app/info lookup for the publish error's review reason failed", "error", qerr)
		return err
	}
	if app == nil {
		return err
	}
	if state, _ := mapOppoAudit(app.AuditStatusName, app.RefuseReason); state != store.AuditRejected {
		return err
	}
	status := app.AuditStatusName
	if code := strings.TrimSpace(app.AuditStatus); code != "" {
		status += "，audit_status=" + code
	}
	switch vn, vc := app.VersionName, strings.TrimSpace(app.VersionCode); {
	case vn != "" && vc != "":
		status += fmt.Sprintf("，版本 %s (%s)", vn, vc)
	case vn+vc != "":
		status += "，版本 " + vn + vc
	}
	if reason := strings.TrimSpace(app.RefuseReason); reason != "" {
		return fmt.Errorf("%w；OPPO 后台审核结果：%s，驳回原因：%s", err, status, reason)
	}
	return fmt.Errorf("%w；OPPO 后台审核结果：%s", err, status)
}

func (s *Store) queryApp(ctx context.Context, pkgName string) (*appData, error) {
	data := url.Values{}
	data.Set("pkg_name", pkgName)
	var resp struct {
		errEnvelope
		Data json.RawMessage `json:"data"`
	}
	httpResp, err := s.call(ctx, func() (*resty.Response, error) {
		return s.client.R().
			SetContext(ctx).
			SetResult(&resp).
			SetQueryParamsFromValues(s.sign(data)).
			Get("/resource/v1/app/info")
	})
	if err != nil {
		return nil, err
	}
	if httpResp.IsError() {
		return nil, store.StatusError("", httpResp.StatusCode(), string(httpResp.Body()))
	}
	if resp.Errno != 0 {
		return nil, fmt.Errorf("[%d] %s", resp.Errno, parseError(httpResp.Body()))
	}
	// OPPO returns data: [] when no app is found under this developer account
	trimmed := strings.TrimSpace(string(resp.Data))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return nil, nil
	}
	var app appData
	if err := json.Unmarshal(resp.Data, &app); err != nil {
		return nil, fmt.Errorf("parse app data: %w", err)
	}
	return &app, nil
}

func (s *Store) uploadAPK(ctx context.Context, apkFile store.PackageFile, rep progress.Reporter) (*uploadResultData, error) {
	return s.uploadFile(ctx, apkFile, "apk", rep)
}

// uploadFile runs OPPO's two-step upload (get-upload-url → multipart POST) for
// any file type. fileType is OPPO's discriminator: "apk", "photo" (images), or
// "resource". The one-time sign is per-file, so each call fetches a fresh one.
// file is the package, or a local image as PackageFile{Path: image}.
func (s *Store) uploadFile(ctx context.Context, file store.PackageFile, fileType string, rep progress.Reporter) (*uploadResultData, error) {
	// Get upload URL
	var urlResp struct {
		errEnvelope
		Data struct {
			UploadURL string `json:"upload_url"`
			Sign      string `json:"sign"`
		} `json:"data"`
	}
	httpResp, err := s.call(ctx, func() (*resty.Response, error) {
		return s.client.R().
			SetContext(ctx).
			SetResult(&urlResp).
			SetQueryParamsFromValues(s.sign(url.Values{})).
			Get("/resource/v1/upload/get-upload-url")
	})
	if err != nil {
		return nil, fmt.Errorf("get-upload-url: %w", err)
	}
	if httpResp.IsError() {
		return nil, store.StatusError("get-upload-url: ", httpResp.StatusCode(), string(httpResp.Body()))
	}
	if urlResp.Errno != 0 || urlResp.Data.UploadURL == "" {
		return nil, fmt.Errorf("get-upload-url: [%d] %s", urlResp.Errno, parseError(httpResp.Body()))
	}

	// Upload file (streamed, with progress reporting)
	fSize, err := file.Size()
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	var uploadResp struct {
		errEnvelope
		Data uploadResultData `json:"data"`
	}
	resp, err := httpx.DoMultipart(ctx, httpx.MultipartRequest{
		Method: http.MethodPost,
		URL:    urlResp.Data.UploadURL,
		Fields: map[string]string{
			"sign": urlResp.Data.Sign,
			"type": fileType,
		},
		Files:    []httpx.FileField{{Field: "file", FileName: file.Name(), Path: file.Path, Size: fSize}},
		Progress: rep,
		Client:   s.uploadClient,
	})
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upload: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if jerr := json.Unmarshal(body, &uploadResp); jerr != nil {
		return nil, fmt.Errorf("decode upload response (HTTP %d): %v: %s",
			resp.StatusCode, jerr, strings.TrimSpace(string(body)))
	}
	if uploadResp.Errno != 0 {
		return nil, fmt.Errorf("upload: [%d] %s", uploadResp.Errno, parseError(body))
	}
	if uploadResp.Data.URL == "" {
		return nil, fmt.Errorf("upload: empty url in response (raw: %s)", strings.TrimSpace(string(body)))
	}
	return &uploadResp.Data, nil
}

func (s *Store) publish(ctx context.Context, req *store.UploadRequest, app *appData, apkInfos []apkInfo) error {
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
	if req.ReleaseTime != nil {
		// Scheduled release (定时发布): online_type 2 = 定时发布, with
		// sche_online_time as a Beijing-local datetime string.
		values.Set("online_type", "2")
		values.Set("sche_online_time", store.BeijingLocalTime(*req.ReleaseTime))
	}
	values.Set("test_desc", "submitted by apkgo")
	// /app/upd validates 软件版权证明 from copyright_url. Accounts that uploaded
	// an electronic copyright certificate (电子版权证书) get an empty copyright_url
	// from /app/info with the URL only in electronic_cert_url, so fall back to it.
	copyrightURL := app.CopyrightURL
	if copyrightURL == "" {
		copyrightURL = app.ElectronicCertURL
	}
	values.Set("copyright_url", copyrightURL)
	values.Set("electronic_cert_url", app.ElectronicCertURL)
	// OPPO's /app/info returns the business contact fields with stray leading
	// whitespace (observed " 2052576764@qq.com"), which /app/upd then rejects
	// as [800002] 商务联系邮箱格式不正确. Trim before echoing them back.
	values.Set("business_username", strings.TrimSpace(app.BusinessUsername))
	values.Set("business_email", strings.TrimSpace(app.BusinessEmail))
	values.Set("business_mobile", strings.TrimSpace(app.BusinessMobile))
	values.Set("age_level", app.AgeLevel)
	values.Set("adaptive_equipment", app.AdaptiveEquipment)
	values.Set("adaptive_type", "2")
	values.Set("customer_contact", strings.TrimSpace(app.CustomerContact))

	var resp struct {
		errEnvelope
	}
	// ctx only for call's logging: a submit already on its way is not
	// cut off.
	httpResp, err := s.call(ctx, func() (*resty.Response, error) {
		return s.client.R().
			SetResult(&resp).
			SetFormDataFromValues(s.sign(values)).
			Post("/resource/v1/app/upd")
	})
	if err != nil {
		return err
	}
	if httpResp.IsError() {
		return fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Errno != 0 {
		return &apiError{Errno: resp.Errno, Msg: parseError(httpResp.Body())}
	}
	return nil
}

// oppoConsoleURL is the OPPO release console — by the time we reach
// pollTaskState the APK has been uploaded and the publish task created,
// so a polling timeout means "task is in flight, finish in the console".
const oppoConsoleURL = "https://open.oppomobile.com"

// taskPollInterval is pollTaskState's delay between task-state queries and
// taskPollBudget how long it keeps querying. Vars so tests can shorten them.
var (
	taskPollInterval = 10 * time.Second
	taskPollBudget   = 10 * time.Minute
)

// taskFinalCheckReserve is kept back from ctx's deadline so that the
// app/info check after the last poll still runs when the caller's timeout
// is shorter than taskPollBudget.
const taskFinalCheckReserve = 15 * time.Second

// pollTaskState waits for OPPO's async publish task to finish. The task
// usually takes 2–3 minutes but has run past 5 (2026-10-15: task_state
// still 1 待处理 after 30 polls), so we poll for up to taskPollBudget,
// cut short by ctx's deadline. If it's still pending then, app/info
// decides: this version already in review (or live) is a success;
// anything else is store_busy — OPPO has the APK and the task, and a
// retry either waits on the same task (911216) or finds the version in
// review (911215).
func (s *Store) pollTaskState(ctx context.Context, pkgName, versionCode string) error {
	interval := taskPollInterval
	start := time.Now()
	stop := start.Add(taskPollBudget)
	if d, ok := ctx.Deadline(); ok && d.Add(-taskFinalCheckReserve).Before(stop) {
		stop = d.Add(-taskFinalCheckReserve)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for time.Until(stop) >= interval {
		select {
		case <-ctx.Done():
			return fmt.Errorf("polling cancelled: %v (APK 已上传，发布任务已创建；可在 OPPO 后台查看进度：%s)", ctx.Err(), oppoConsoleURL)
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
		httpResp, err := s.call(ctx, func() (*resty.Response, error) {
			return s.client.R().
				SetContext(ctx).
				SetResult(&resp).
				SetFormDataFromValues(s.sign(data)).
				Post("/resource/v1/app/task-state")
		})
		if err != nil {
			return err
		}
		if httpResp.IsError() {
			return store.StatusError("", httpResp.StatusCode(), string(httpResp.Body()))
		}
		// Surface envelope-level errors immediately. Without this, an
		// auth/sign failure would loop silently for the full polling
		// window before reporting a useless "polling timed out".
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

	waited := time.Since(start).Round(time.Second)
	if s.versionInReview(ctx, pkgName, versionCode) {
		ctxlog.FromContext(ctx).Info("oppo publish task still pending but the version is already in review", "waited", waited)
		return nil
	}
	return store.Categorize(store.CategoryStoreBusy, fmt.Errorf(
		"publish task still pending after %s (APK 已上传，发布任务已创建；稍后重试即可，重试会接着等这个任务；也可在 OPPO 后台查看进度：%s)",
		waited, oppoConsoleURL))
}

// versionInReview reports whether app/info already shows versionCode in
// review or live, i.e. the publish task got through even though
// task-state didn't report it in time. Only reviewing/approved count: an
// earlier rejection of the same version (a resubmission, as on
// 2026-10-15) says nothing about this task. A failed query counts as not
// in review.
func (s *Store) versionInReview(ctx context.Context, pkgName, versionCode string) bool {
	app, err := s.queryApp(ctx, pkgName)
	if err != nil {
		ctxlog.FromContext(ctx).Warn("oppo app/info check after task-state polling failed", "error", err)
		return false
	}
	if app == nil || strings.TrimSpace(app.VersionCode) != versionCode {
		return false
	}
	state, _ := mapOppoAudit(app.AuditStatusName, app.RefuseReason)
	return state == store.AuditReviewing || state == store.AuditApproved
}

// sign adds common params and HMAC-SHA256 signature. Signing data again
// (call resending it) replaces all three.
func (s *Store) sign(data url.Values) url.Values {
	data.Del("api_sign")
	s.mu.Lock()
	data.Set("access_token", s.accessToken)
	s.mu.Unlock()
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
	ElectronicCertURL string `json:"electronic_cert_url"` // 电子版权证书; copyright_url 已弃用,改用此字段
	BusinessUsername  string `json:"business_username"`
	BusinessEmail     string `json:"business_email"`
	BusinessMobile    string `json:"business_mobile"`
	AgeLevel          string `json:"age_level"`
	AdaptiveEquipment string `json:"adaptive_equipment"`
	CustomerContact   string `json:"customer_contact"`
	AuditStatus       string `json:"audit_status"`      // numeric code (审核状态对照表 not published in the API传包 docs)
	AuditStatusName   string `json:"audit_status_name"` // human label, e.g. "上线" / "审核中"
	RefuseReason      string `json:"refuse_reason"`
	VersionName       string `json:"version_name"` // 版本名称
	VersionCode       string `json:"version_code"` // 版本号 (returned as a string)
}

type uploadResultData struct {
	URL string `json:"url"`
	MD5 string `json:"md5"`
	ID  string `json:"id"`
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
//     the HMAC-SHA256 signature is being computed correctly
//     (without a sig that lines up server-side, app/info
//     returns errno=… instead of the package data)
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

	app, err := s.queryApp(ctx, hint.Package)
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
