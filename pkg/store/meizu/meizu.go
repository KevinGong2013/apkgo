// Package meizu implements the Meizu (Flyme) app store via the open
// publish API launched in December 2025 (open.flyme.cn/docs?id=333).
// Host developer.meizu.com; auth is clientId/clientSecret → accessToken,
// with every call carrying SHA-256-signed headers.
package meizu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"

	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

const baseURL = "https://developer.meizu.com"

func init() {
	store.Register("meizu", store.ConfigSchema{
		Name:       "meizu",
		ConsoleURL: "https://open.flyme.cn",
		Fields: []store.FieldSchema{
			{Key: "client_id", Required: true, Desc: "Meizu open platform client ID (客户端凭证)"},
			{Key: "client_secret", Required: true, Desc: "Meizu open platform client secret"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("meizu", diagnose)
	store.RegisterAuditor("meizu", audit)
}

// intish tolerates the envelope `code` arriving as either a JSON number
// (as documented) or a quoted string (as Meizu's sibling doc-wiki API
// actually returns), so a format drift doesn't break error reporting.
type intish int

func (v *intish) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*v = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("non-numeric code %q", s)
	}
	*v = intish(n)
	return nil
}

// envelope is the common {code, msg, value} response wrapper. The docs
// name the message field `msg` in tables but `message` in prose, so both
// are parsed.
type envelope struct {
	Code    intish `json:"code"`
	Msg     string `json:"msg"`
	Message string `json:"message"`
}

// errOf turns a non-200 envelope into an error carrying Meizu's code and
// human message, e.g. "[113033] 签名错误".
func (e *envelope) errOf() error {
	msg := e.Msg
	if msg == "" {
		msg = e.Message
	}
	return fmt.Errorf("[%d] %s", int(e.Code), msg)
}

type Store struct {
	client       *resty.Client
	uploadClient *http.Client // multipart uploads; shares the relaxed-handshake transport
	clientID     string
	clientSecret string
	accessToken  string
}

func New(cfg map[string]string) (*Store, error) {
	clientID := strings.TrimSpace(cfg["client_id"])
	clientSecret := strings.TrimSpace(cfg["client_secret"])
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required")
	}

	// developer.meizu.com is observed to take >10s (net/http's default
	// TLS handshake timeout) to complete a handshake from outside China,
	// so the cutoff is raised rather than failing CI runs on latency.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSHandshakeTimeout = 60 * time.Second

	s := &Store{
		client:       resty.New().SetBaseURL(baseURL).SetTransport(transport),
		uploadClient: &http.Client{Timeout: 30 * time.Minute, Transport: transport},
		clientID:     clientID,
		clientSecret: clientSecret,
	}
	if err := s.fetchToken(); err != nil {
		return nil, store.Categorize(store.CategoryAuthFailed, fmt.Errorf("auth: %w", err))
	}
	return s, nil
}

// fetchToken exchanges clientId+clientSecret for an accessToken. Unlike
// the signed API calls, the token endpoint authenticates with the raw
// credentials sent as request headers.
func (s *Store) fetchToken() error {
	var resp struct {
		envelope
		Value struct {
			AccessToken string `json:"accessToken"`
		} `json:"value"`
	}
	httpResp, err := s.client.R().
		SetHeaders(map[string]string{
			"clientId":     s.clientID,
			"clientSecret": s.clientSecret,
		}).
		SetResult(&resp).
		Get("/open/api/v1/token")
	if err != nil {
		return err
	}
	if httpResp.IsError() {
		return fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Code != 200 || resp.Value.AccessToken == "" {
		return resp.errOf()
	}
	s.accessToken = resp.Value.AccessToken
	return nil
}

// signedHeaders builds the common request headers for one API call.
// sign = SHA-256 hex of the sorted "k=v" pairs of {traceId, clientId,
// timestamp, uri} joined with "&", suffixed with ":"+clientSecret. The
// request uri participates in the signature but query params do not.
func (s *Store) signedHeaders(uri string) map[string]string {
	traceID := uuid.NewString()
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)

	params := map[string]string{
		"traceId":   traceID,
		"clientId":  s.clientID,
		"timestamp": timestamp,
		"uri":       uri,
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + params[k]
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "&") + ":" + s.clientSecret))

	return map[string]string{
		"traceId":     traceID,
		"clientId":    s.clientID,
		"timestamp":   timestamp,
		"sign":        hex.EncodeToString(sum[:]),
		"accessToken": s.accessToken,
	}
}

// apiGet issues a signed GET and decodes the response into out, which
// must embed envelope so the wrapper error check can run here.
func (s *Store) apiGet(ctx context.Context, uri string, query map[string]string, out interface {
	env() *envelope
}) error {
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetHeaders(s.signedHeaders(uri)).
		SetQueryParams(query).
		SetResult(out).
		Get(uri)
	if err != nil {
		return err
	}
	if httpResp.IsError() {
		return fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if out.env().Code != 200 {
		return out.env().errOf()
	}
	return nil
}

func (s *Store) Name() string { return "meizu" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	verID, err := s.upload(ctx, req)
	if err != nil {
		if store.IsAlreadyDone(err) {
			return store.NewResultC(s.Name(), start, store.CategoryAlreadyDone)
		}
		return store.ErrResult(s.Name(), start, err)
	}
	res := store.NewResult(s.Name(), start)
	if verID != 0 {
		res.ExternalID = strconv.FormatInt(verID, 10)
	}
	return res
}

func (s *Store) upload(ctx context.Context, req *store.UploadRequest) (int64, error) {
	rep := progress.Safe(req.Progress)

	// 1. Resolve the app and its current metadata. The publish API can
	// only submit new versions of an existing app — first-time listing
	// (with qualifications, ICP filing, screenshots…) is console-only.
	rep.Phase("query")
	app, err := s.findApp(ctx, req.PackageName)
	if err != nil {
		return 0, fmt.Errorf("query app: %w", err)
	}
	if app == nil {
		return 0, fmt.Errorf("no app found for package %s under this developer account (首次上架需在魅族开发者中心完成)", req.PackageName)
	}
	if (app.Status == statusPendingReview || app.Status == statusInReview) && app.VersionName == req.VersionName {
		return 0, &store.AlreadyDoneError{Reason: fmt.Sprintf("version %s already in Meizu review queue", req.VersionName)}
	}

	det, err := s.detail(ctx, app.VerID)
	if err != nil {
		return 0, fmt.Errorf("query app detail: %w", err)
	}

	// 2. Upload the APK. Meizu accepts a single 64-bit package (32-bit
	// uploads are rejected with code 113030), so a split-arch invocation
	// sends the 64-bit artifact.
	apkPath := req.FilePath
	if req.File64Path != "" {
		apkPath = req.File64Path
	}
	rep.Phase("uploading")
	packageURL, err := s.uploadAPK(ctx, apkPath, rep)
	if err != nil {
		return 0, fmt.Errorf("upload apk: %w", err)
	}

	// 3. Submit for review, echoing the currently-listed metadata with
	// the new package and release notes. A latest version sitting in
	// "审核不通过" must go through failapp/update instead of publish
	// (which would fail with 113042/113046).
	rep.Phase("publishing")
	body := det.publishBody(packageURL, req.ReleaseNotes)
	uri := "/open/api/v1/app/publish"
	if app.Status == statusRejected {
		uri = "/open/api/v1/app/failapp/update"
		body["verId"] = app.VerID
	}
	verID, err := s.submit(ctx, uri, body)
	if err != nil {
		return 0, fmt.Errorf("publish: %w", err)
	}
	return verID, nil
}

// findApp pages through the developer's app list (limit is capped at 10
// by the API) looking for the package name.
func (s *Store) findApp(ctx context.Context, pkgName string) (*appItem, error) {
	const pageSize = 10
	for start := 0; ; start += pageSize {
		var resp appListResp
		err := s.apiGet(ctx, "/open/api/v1/app/list", map[string]string{
			"start": strconv.Itoa(start),
			"limit": strconv.Itoa(pageSize),
		}, &resp)
		if err != nil {
			return nil, err
		}
		for i := range resp.Value.Data {
			if resp.Value.Data[i].PkgName == pkgName {
				return &resp.Value.Data[i], nil
			}
		}
		if len(resp.Value.Data) == 0 || start+pageSize >= resp.Value.Total {
			return nil, nil
		}
	}
}

func (s *Store) detail(ctx context.Context, verID int64) (*appDetail, error) {
	var resp appDetailResp
	err := s.apiGet(ctx, "/open/api/v1/app/detail", map[string]string{
		"verId": strconv.FormatInt(verID, 10),
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Value, nil
}

// uploadAPK streams the APK to /app/apk/upload and returns the stored
// file name, which publish takes as packageUrl.
func (s *Store) uploadAPK(ctx context.Context, filePath string, rep progress.Reporter) (string, error) {
	rc, fSize, err := progress.WrapFile(filePath, rep)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer rc.Close()
	rep.Total(fSize)

	const uri = "/open/api/v1/app/apk/upload"
	resp, err := httpx.DoMultipart(ctx, httpx.MultipartRequest{
		Method:  http.MethodPost,
		URL:     baseURL + uri,
		Headers: s.signedHeaders(uri),
		Files:   []httpx.FileField{{Field: "file", FileName: filepath.Base(filePath), Reader: rc, Size: fSize}},
		Client:  s.uploadClient,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var uploadResp struct {
		envelope
		Value struct {
			DestFileName string `json:"destFileName"`
		} `json:"value"`
	}
	if jerr := json.Unmarshal(body, &uploadResp); jerr != nil {
		return "", fmt.Errorf("decode upload response (HTTP %d): %v: %s", resp.StatusCode, jerr, strings.TrimSpace(string(body)))
	}
	if uploadResp.Code != 200 || uploadResp.Value.DestFileName == "" {
		return "", uploadResp.errOf()
	}
	return uploadResp.Value.DestFileName, nil
}

// submit POSTs the publish/failapp-update JSON body and returns the new
// version id.
func (s *Store) submit(ctx context.Context, uri string, body map[string]any) (int64, error) {
	var resp struct {
		envelope
		Value struct {
			VerID int64 `json:"verId"`
		} `json:"value"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetHeaders(s.signedHeaders(uri)).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		SetResult(&resp).
		Post(uri)
	if err != nil {
		return 0, err
	}
	if httpResp.IsError() {
		return 0, fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Code != 200 {
		return 0, resp.errOf()
	}
	return resp.Value.VerID, nil
}

// App status codes (附录 3.12.6 应用状态选项).
const (
	statusPendingReview = 20  // 待审核
	statusRejected      = 30  // 审核不通过
	statusOnSale        = 50  // 上架
	statusOffSale       = 70  // 下架
	statusInReview      = 100 // 审核中
)

var statusLabels = map[int]string{
	statusPendingReview: "待审核",
	statusRejected:      "审核不通过",
	statusOnSale:        "上架",
	statusOffSale:       "下架",
	statusInReview:      "审核中",
}

// audit is registered with `apkgo audit`. It maps the latest version's
// status from the app list to the unified state. The list also carries a
// replaceStatus for in-place modifications of an on-sale app, but its
// zero value is indistinguishable from "no pending modification", so
// only the primary status is mapped.
func audit(ctx context.Context, cfg map[string]string, q store.AuditQuery) store.AuditResult {
	res := store.AuditResult{Store: "meizu"}
	s, err := New(cfg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	app, err := s.findApp(ctx, q.Package)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if app == nil {
		res.Error = fmt.Sprintf("no app found for package %s under this developer account", q.Package)
		return res
	}
	res.VersionName = app.VersionName
	switch app.Status {
	case statusPendingReview, statusInReview:
		res.State = store.AuditReviewing
	case statusRejected:
		res.State = store.AuditRejected
	case statusOnSale:
		res.State = store.AuditApproved
	case statusOffSale:
		res.State = store.AuditWithdrawn
	default:
		res.State = store.AuditUnknown
	}
	if label, ok := statusLabels[app.Status]; ok {
		res.Detail = label
	} else {
		res.Detail = fmt.Sprintf("status=%d", app.Status)
	}
	return res
}

// diagnose is registered with `apkgo doctor`. Probes:
//
//  1. token    — clientId/clientSecret are accepted by /open/api/v1/token
//  2. app-list — the signed-header scheme works and the package exists
//     under this developer account
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 2)

	s, err := New(cfg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "token", Status: "fail", Error: err.Error()})
		return probes
	}
	probes = append(probes, store.Probe{Name: "token", Status: "ok", Detail: "accessToken issued"})

	if hint.Package == "" {
		probes = append(probes, store.Probe{Name: "app-list", Status: "skip", Detail: "needs --package or --file"})
		return probes
	}

	app, err := s.findApp(ctx, hint.Package)
	if err != nil {
		probes = append(probes, store.Probe{Name: "app-list", Status: "fail", Error: err.Error()})
		return probes
	}
	if app == nil {
		probes = append(probes, store.Probe{Name: "app-list", Status: "fail", Error: fmt.Sprintf("no app found for package %s under this developer account", hint.Package)})
		return probes
	}
	probes = append(probes, store.Probe{Name: "app-list", Status: "ok", Detail: fmt.Sprintf("%s → %q", hint.Package, app.Name)})
	return probes
}

// Data types

type appItem struct {
	ID          int64  `json:"id"`
	VerID       int64  `json:"verId"` // latest version id
	PkgName     string `json:"pkgName"`
	Name        string `json:"name"`
	VersionName string `json:"versionName"`
	Status      int    `json:"status"`
}

type appListResp struct {
	envelope
	Value struct {
		Total int       `json:"total"`
		Data  []appItem `json:"data"`
	} `json:"value"`
}

func (r *appListResp) env() *envelope { return &r.envelope }

// appDetail is the full version record from /app/detail — everything the
// publish endpoint requires, so an update can round-trip the listed
// metadata unchanged. Field names ending in pinyin initialisms are the
// ICP-filing subject fields (主办者/运营者) and are echoed verbatim.
type appDetail struct {
	ID             int64    `json:"id"`
	VerID          int64    `json:"verId"`
	Status         int      `json:"status"`
	Name           string   `json:"name"`
	VersionName    string   `json:"versionName"`
	PackageName    string   `json:"packageName"`
	AppDescription string   `json:"appDescription"`
	VerDescription string   `json:"verDescription"`
	Keyword        string   `json:"keyword"`
	RecommendDesc  string   `json:"recommendDesc"`
	AuthorName     string   `json:"authorName"`
	DevContact     string   `json:"devContact"`
	CategoryID     int64    `json:"categoryId"`
	Category2ID    int64    `json:"category2Id"`
	TagID          int64    `json:"tagId"`
	Icon           string   `json:"icon"`
	ScreenShots    []string `json:"screenShots"`
	Certificates   string   `json:"certificates"` // comma-separated; publish wants a list
	AgeBracket     int      `json:"ageBracket"`
	PrivacyPolicy  string   `json:"privacyPolicyUrl"`
	SoftwareAuthor string   `json:"softwareAuthorNum"`
	Qualifcation   string   `json:"qualifcation"` // (sic) misspelled in the API
	Dwmc           string   `json:"dwmc"`
	Zjlx           int64    `json:"zjlx"`
	Zjhm           string   `json:"zjhm"`
	Yyzzjlx        int64    `json:"yyzzjlx"`
	Yyzmc          string   `json:"yyzmc"`
	Yyzzjhm        string   `json:"yyzzjhm"`
	Yyzlxrlxfs     string   `json:"yyzlxrlxfs"`
	Yylb           string   `json:"yylb"`
	ZbzShengID     int64    `json:"zbzShengId"`
}

type appDetailResp struct {
	envelope
	Value appDetail `json:"value"`
}

func (r *appDetailResp) env() *envelope { return &r.envelope }

// publishBody assembles the /app/publish (or failapp/update) request from
// the currently-listed detail, swapping in the freshly-uploaded package
// and the new release notes.
func (d *appDetail) publishBody(packageURL, releaseNotes string) map[string]any {
	verDesc := releaseNotes
	if verDesc == "" {
		verDesc = d.VerDescription
	}
	catid := d.CategoryID
	if catid == 0 {
		catid = 1 // 一级分类固定为1（应用）
	}
	certificates := []string{}
	for _, c := range strings.Split(d.Certificates, ",") {
		if c = strings.TrimSpace(c); c != "" {
			certificates = append(certificates, c)
		}
	}
	return map[string]any{
		"appName":           d.Name,
		"appDesc":           d.AppDescription,
		"verDesc":           verDesc,
		"catid":             catid,
		"cat2id":            d.Category2ID,
		"tagId":             d.TagID,
		"authorName":        d.AuthorName,
		"packageUrl":        packageURL,
		"icon":              d.Icon,
		"screenShots":       d.ScreenShots,
		"certificates":      certificates,
		"privacyPolicyUrl":  d.PrivacyPolicy,
		"keyword":           d.Keyword,
		"recommendDesc":     d.RecommendDesc,
		"softwareAuthorNum": d.SoftwareAuthor,
		"devContact":        d.DevContact,
		"ageBracket":        d.AgeBracket,
		"qualifcation":      d.Qualifcation,
		"dwmc":              d.Dwmc,
		"zjlx":              d.Zjlx,
		"zjhm":              d.Zjhm,
		"yyzzjlx":           d.Yyzzjlx,
		"yyzmc":             d.Yyzmc,
		"yyzzjhm":           d.Yyzzjhm,
		"yyzlxrlxfs":        d.Yyzlxrlxfs,
		"yylb":              d.Yylb,
		"zbzShengId":        d.ZbzShengID,
	}
}
