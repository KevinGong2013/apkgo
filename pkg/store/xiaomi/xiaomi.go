package xiaomi

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
	"github.com/KevinGong2013/apkgo/v4/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

//go:embed dev.api.public.cer
var defaultCertPEM []byte

const xiaomiBaseURL = "https://api.developer.xiaomi.com/devupload"

func init() {
	store.Register("xiaomi", store.ConfigSchema{
		Name:                     "xiaomi",
		ConsoleURL:               "https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1134",
		SupportsScheduledRelease: true,
		Fields: []store.FieldSchema{
			{Key: "email", Required: true, Desc: "Xiaomi developer account email (mapped to userName)"},
			{Key: "private_key", Required: true, Desc: "Xiaomi API private key (the value the upload SDK calls 'password')"},
			{Key: "cert", Required: false, Desc: "Xiaomi public key certificate (raw PEM or base64); optional, defaults to built-in dev.api.public.cer"},
			{Key: "cert_file", Required: false, Desc: "Path to Xiaomi public key certificate file (.cer/.pem); optional, defaults to built-in dev.api.public.cer"},
		},
		Listing: listingSpec,
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("xiaomi", diagnose)
	store.RegisterAuditor("xiaomi", audit)
}

// audit is registered with `apkgo audit`. Xiaomi exposes no review-status
// API (the developer FAQ states it is "暂时没有开放"), so we infer the state
// by version comparison: /dev/query returns the version currently live on
// the store, and AuditQuery carries the version we last submitted. A
// submitted version higher than the live one means our update is still
// pending review (reviewing); equal — or our hint missing — means the live
// store already serves it (approved). Xiaomi can't distinguish a rejected
// submission from an in-review one (both leave the live version unchanged),
// so a pending version is reported as reviewing.
func audit(ctx context.Context, cfg map[string]string, q store.AuditQuery) store.AuditResult {
	res := store.AuditResult{Store: "xiaomi"}
	s, err := New(cfg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	info, err := s.query(ctx, q.Package)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	const inferred = "小米无审核状态接口，依据版本推断"
	if info == nil {
		// Never uploaded under this account: a submitted version we know about
		// is freshly pending; otherwise there's nothing to report.
		if q.VersionCode > 0 {
			res.State = store.AuditReviewing
			res.Detail = "首次提交，等待小米审核（" + inferred + "）"
			res.VersionName, res.VersionCode = q.VersionName, q.VersionCode
		} else {
			res.State = store.AuditUnknown
			res.Detail = "该应用尚未在小米上架"
		}
		return res
	}
	res.LiveVersionName = info.VersionName
	res.LiveVersionCode = int32(info.VersionCode)
	if q.VersionCode > int32(info.VersionCode) {
		// Our submission is newer than what's live → still pending review.
		res.State = store.AuditReviewing
		res.Detail = fmt.Sprintf("提交版本 %d 高于在架版本 %d，等待上线（%s）", q.VersionCode, info.VersionCode, inferred)
		res.VersionName, res.VersionCode = q.VersionName, q.VersionCode
	} else {
		// Live version already matches (or exceeds) our submission → approved.
		res.State = store.AuditApproved
		res.Detail = "在架版本已是最新（" + inferred + "）"
		res.VersionName, res.VersionCode = info.VersionName, int32(info.VersionCode)
	}
	return res
}

type Store struct {
	client       *resty.Client
	uploadClient *http.Client // streamed file uploads; nil = httpx's default
	baseURL      string
	email        string
	privateKey   string
	pubKey       *rsa.PublicKey
}

func New(cfg map[string]string) (*Store, error) {
	email := strings.TrimSpace(cfg["email"])
	privateKey := strings.TrimSpace(cfg["private_key"])
	if email == "" || privateKey == "" {
		return nil, fmt.Errorf("email and private_key are required")
	}

	certInline := strings.TrimSpace(cfg["cert"])
	certFile := strings.TrimSpace(cfg["cert_file"])
	pubKey, err := func() (*rsa.PublicKey, error) {
		if certInline != "" {
			return loadCert(certInline)
		}
		if certFile != "" {
			return loadCertFromFile(certFile)
		}
		return loadCert(string(defaultCertPEM))
	}()
	if err != nil {
		return nil, fmt.Errorf("load xiaomi public key: %w", err)
	}

	client := resty.New().
		SetBaseURL(xiaomiBaseURL)
	trace := httptrace.ForStore("xiaomi", cfg)
	trace.Client(client.GetClient())

	return &Store{
		client:       client,
		uploadClient: trace.NewClient(30 * time.Minute),
		baseURL:      xiaomiBaseURL,
		email:        email,
		privateKey:   privateKey,
		pubKey:       pubKey,
	}, nil
}

func (s *Store) Name() string { return "xiaomi" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(ctx context.Context, req *store.UploadRequest) error {
	rep := progress.Safe(req.Progress)

	// Query existing version
	rep.Phase("query")
	info, err := s.query(ctx, req.PackageName)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}

	synchroType := 1 // update
	if info == nil {
		synchroType = 0 // new app
	} else if info.VersionCode >= int(req.VersionCode) {
		return fmt.Errorf("store version (%d) >= local version (%d)", info.VersionCode, req.VersionCode)
	}

	// Updates must never rename the store listing: reuse the name already
	// registered on the console (from /dev/query) instead of the APK
	// label — an i18n APK's base values/ label is often English, and
	// renaming is a console decision, not an upload side effect (#48).
	// req is per-store (both the CLI uploader and apkgo-cloud build one
	// per store), so mutating it here is safe.
	if info != nil && info.AppName != "" {
		req.AppName = info.AppName
	}

	// /dev/push marks `icon` as required for every synchroType (new app and
	// update alike — only screenshots are new-app-only), and whatever we
	// send replaces the icon on the console. A listing icon is the user's
	// own file: send it as is and never delete it. Otherwise extract the
	// densest launcher icon from the APK (#51) into a temp file.
	var iconPath string
	if req.Listing != nil && req.Listing.Icon != "" {
		iconPath = req.Listing.Icon
	} else {
		rep.Phase("icon")
		iconPath, err = extractIcon(req.Package())
		if err != nil {
			return fmt.Errorf("extract icon: %w", err)
		}
		defer os.RemoveAll(filepath.Dir(iconPath))
	}

	// Push
	return s.push(ctx, synchroType, req, iconPath, rep)
}

// resultRateLimited is xiaomi's 「请求频率太快」. Xiaomi documents neither the
// limit nor what it counts (account or source address): 2026-10-16 a
// /dev/query 54ms after another one for the same account got it, and the
// upload failed with it in place of the real error.
const resultRateLimited = -20035

// queryRateLimitWaits are the pauses before query asks again after
// -20035, one per retry. /dev/query only reads, so asking again is safe,
// and a few seconds clears the near-simultaneous calls that trip the limit
// (a preflight query right before the upload's, several jobs on one
// account or one host). If it still says -20035 the error is store_busy and
// the orchestrator retries the store minutes later. A var so tests can
// shorten it.
var queryRateLimitWaits = []time.Duration{2 * time.Second, 5 * time.Second}

// query looks up the package on the account, retrying -20035 after
// queryRateLimitWaits.
func (s *Store) query(ctx context.Context, packageName string) (*packageInfo, error) {
	for attempt := 0; ; attempt++ {
		info, result, err := s.queryOnce(ctx, packageName)
		if result != resultRateLimited || attempt >= len(queryRateLimitWaits) {
			return info, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(queryRateLimitWaits[attempt]):
		}
	}
}

// queryOnce sends one /dev/query and also returns xiaomi's result code
// (0 when the request didn't get that far).
func (s *Store) queryOnce(ctx context.Context, packageName string) (*packageInfo, int, error) {
	body := s.encode(map[string]any{
		"packageName": packageName,
		"userName":    s.email,
	}, nil)

	var resp struct {
		Result      int          `json:"result"`
		Message     string       `json:"message,omitempty"`
		Reason      string       `json:"reason,omitempty"`
		PackageInfo *packageInfo `json:"packageInfo"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetBody(body.Encode()).
		SetResult(&resp).
		Post("/dev/query")
	if err != nil {
		return nil, 0, err
	}
	if httpResp.IsError() {
		return nil, 0, fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Result != 0 {
		msg := resp.Message
		if msg == "" {
			msg = resp.Reason
		}
		if msg == "" {
			msg = strings.TrimSpace(string(httpResp.Body()))
		}
		return nil, resp.Result, store.Categorize(classifyXiaomi(resp.Result, msg),
			fmt.Errorf("[%d] %s", resp.Result, msg))
	}
	return resp.PackageInfo, 0, nil
}

func (s *Store) push(ctx context.Context, synchroType int, req *store.UploadRequest, iconPath string, rep progress.Reporter) error {
	appInfo := map[string]any{
		"appName":     req.AppName,
		"packageName": req.PackageName,
		"updateDesc":  req.ReleaseNotes,
	}
	if req.ReleaseTime != nil {
		// Scheduled release (定时上线): onlineTime is a Unix millisecond
		// timestamp (absolute instant, timezone-independent).
		appInfo["onlineTime"] = req.ReleaseTime.UnixMilli()
	}
	// Listing (商店资料): only non-empty fields are sent, so the store keeps
	// its current value for the rest. The icon arrives as iconPath (see
	// upload); screenshots become screenshot_1..N parts below.
	// NOTE: the /dev/push doc marks brief/desc/screenshot_1-3 only as
	// "新增时必选" (synchroType=0); whether they take effect on an update
	// (synchroType=1, apkgo's normal path) still needs verifying against a
	// real account.
	if l := req.Listing; l != nil {
		if l.Brief != "" {
			appInfo["brief"] = l.Brief
		}
		if l.Description != "" {
			appInfo["desc"] = l.Description
		}
	}
	shots := screenshotFiles(req.Listing)

	// Order matters only for reproducibility, not for xiaomi: keep the sig
	// list in the order the /dev/push docs list the parts (apk, secondApk,
	// icon, screenshot_1..N) rather than letting map iteration shuffle it
	// per run. The doc requires every uploaded file, screenshots included,
	// to be signed.
	apkFile, apk64File := req.Package(), req.Package64()
	iconFile := store.PackageFile{Path: iconPath}
	files := []sigFile{{name: "apk", file: apkFile}}
	if apk64File.Path != "" {
		files = append(files, sigFile{name: "secondApk", file: apk64File})
	}
	files = append(files, sigFile{name: "icon", file: iconFile})
	files = append(files, shots...)

	body := s.encode(map[string]any{
		"synchroType": synchroType,
		"userName":    s.email,
		"appInfo":     appInfo,
	}, files)

	// Every part is sent from its path, so DoMultipart reports the bytes of
	// all of them to the same reporter: set Total once to their sum.
	rep.Phase("uploading")
	apkSize, err := apkFile.Size()
	if err != nil {
		return fmt.Errorf("stat apk: %w", err)
	}
	iconSize, err := iconFile.Size()
	if err != nil {
		return fmt.Errorf("stat icon: %w", err)
	}
	var apk64Size int64
	if apk64File.Path != "" {
		if apk64Size, err = apk64File.Size(); err != nil {
			return fmt.Errorf("stat apk64: %w", err)
		}
	}

	shotParts := make([]httpx.FileField, 0, len(shots))
	var shotsSize int64
	for _, f := range shots {
		size, err := f.file.Size()
		if err != nil {
			return fmt.Errorf("stat %s: %w", f.name, err)
		}
		shotParts = append(shotParts, httpx.FileField{Field: f.name, FileName: f.file.Name(), Path: f.file.Path, Size: size})
		shotsSize += size
	}
	rep.Total(apkSize + iconSize + apk64Size + shotsSize)

	fields := make(map[string]string, len(body))
	for k := range body {
		fields[k] = body.Get(k)
	}
	parts := []httpx.FileField{
		{Field: "apk", FileName: apkFile.Name(), Path: apkFile.Path, Size: apkSize},
		{Field: "icon", FileName: iconFile.Name(), Path: iconFile.Path, Size: iconSize},
	}
	if apk64File.Path != "" {
		parts = append(parts, httpx.FileField{Field: "secondApk", FileName: apk64File.Name(), Path: apk64File.Path, Size: apk64Size})
	}
	parts = append(parts, shotParts...)

	pushResp, err := httpx.DoMultipart(ctx, httpx.MultipartRequest{
		Method:   http.MethodPost,
		URL:      s.baseURL + "/dev/push",
		Fields:   fields,
		Files:    parts,
		Progress: rep,
		Client:   s.uploadClient,
	})
	if err != nil {
		return err
	}
	defer pushResp.Body.Close()
	respBody, _ := io.ReadAll(pushResp.Body)
	if pushResp.StatusCode >= 400 {
		return fmt.Errorf("push failed: http %d: %s", pushResp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var resp struct {
		Result  int    `json:"result"`
		Message string `json:"message,omitempty"`
	}
	if jerr := json.Unmarshal(respBody, &resp); jerr != nil {
		return fmt.Errorf("decode push response (HTTP %d): %v: %s",
			pushResp.StatusCode, jerr, strings.TrimSpace(string(respBody)))
	}
	if resp.Result != 0 {
		return store.Categorize(classifyXiaomi(resp.Result, resp.Message),
			fmt.Errorf("push failed: %s", resp.Message))
	}
	return nil
}

// classifyXiaomi categorises known xiaomi result codes / message
// patterns. Cloud orchestrators key off the Category to decide
// whether to retry, surface to a human, etc.
func classifyXiaomi(result int, msg string) store.Category {
	switch {
	case strings.Contains(msg, "签名不一致"):
		return store.CategoryPolicyBlock
	case result == -7: // 应用归属关系异常
		return store.CategoryConfigInvalid
	case result == -20014: // 密码错误: private_key doesn't match the email
		return store.CategoryAuthFailed
	case result == resultRateLimited:
		return store.CategoryStoreBusy
	}
	return store.CategoryUnknown
}

// sigFile is one uploaded part as it appears in the SIG hash list: the
// multipart field name plus the file whose md5 is signed — a package, whose
// md5 may be a given fact, or a local image (no facts: its md5 is read
// from the file).
type sigFile struct {
	name string
	file store.PackageFile
}

// encode builds form values with RSA-encrypted SIG. files is signed in the
// given order.
func (s *Store) encode(params map[string]any, files []sigFile) url.Values {
	requestData, _ := json.Marshal(params)
	form := url.Values{}
	form.Set("RequestData", string(requestData))

	sigs := []map[string]string{{
		"name": "RequestData",
		"hash": md5hex(requestData),
	}}

	for _, f := range files {
		if f.file.Path == "" {
			continue
		}
		hash, err := f.file.MD5()
		if err != nil {
			continue
		}
		sigs = append(sigs, map[string]string{"name": f.name, "hash": hash})
	}

	sigPayload, _ := json.Marshal(map[string]any{
		"sig":      sigs,
		"password": s.privateKey,
	})

	encrypted, _ := rsaEncrypt(sigPayload, s.pubKey)
	form.Set("SIG", encrypted)

	return form
}

// --- Crypto helpers ---

// loadCert accepts either:
//   - the raw PEM-encoded X.509 certificate (string contains "-----BEGIN"), or
//   - inline base64-encoded PEM (handy for env vars / CI secrets)
//
// and returns the embedded RSA public key. If neither cert nor cert_file is
// configured, the embedded dev.api.public.cer is used by default.
func loadCert(raw string) (*rsa.PublicKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty cert")
	}
	var pemBytes []byte
	if strings.Contains(raw, "BEGIN") {
		pemBytes = []byte(raw)
	} else {
		var err error
		for _, dec := range []*base64.Encoding{
			base64.StdEncoding, base64.RawStdEncoding,
			base64.URLEncoding, base64.RawURLEncoding,
		} {
			pemBytes, err = dec.DecodeString(raw)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("decode base64: %w", err)
		}
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	if !cert.NotAfter.IsZero() && time.Now().After(cert.NotAfter) {
		return nil, fmt.Errorf("certificate expired on %s — re-download from dev.mi.com", cert.NotAfter.Format("2006-01-02"))
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return pub, nil
}

func loadCertFromFile(path string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return loadCert(string(data))
}

func rsaEncrypt(data []byte, pub *rsa.PublicKey) (string, error) {
	const blockSize = 117 // 1024/8 - 11
	var encrypted []byte
	for len(data) > 0 {
		chunk := blockSize
		if len(data) < chunk {
			chunk = len(data)
		}
		enc, err := rsa.EncryptPKCS1v15(rand.Reader, pub, data[:chunk])
		if err != nil {
			return "", err
		}
		encrypted = append(encrypted, enc...)
		data = data[chunk:]
	}
	return fmt.Sprintf("%x", encrypted), nil
}

func md5hex(data []byte) string {
	return fmt.Sprintf("%x", md5.Sum(data))
}

// extractIcon writes the package's launcher icon (PackageFile.LauncherIcon:
// the densest one it ships, so the console's HD icon isn't overwritten
// with the blurry 48px mdpi variant, #51) as a PNG into a new temp
// directory and returns its path; the caller removes the directory. The
// file keeps the name /dev/push has always been sent, apkgo_icon_tmp.png.
func extractIcon(pf store.PackageFile) (string, error) {
	icon, err := pf.LauncherIcon()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, icon); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "apkgo-xiaomi-icon-")
	if err != nil {
		return "", err
	}
	iconPath := filepath.Join(dir, "apkgo_icon_tmp.png")
	if err := os.WriteFile(iconPath, buf.Bytes(), 0644); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return iconPath, nil
}

type packageInfo struct {
	AppName     string `json:"appName"`
	PackageName string `json:"packageName"`
	VersionCode int    `json:"versionCode"`
	VersionName string `json:"versionName"`
}

// probePackage is what diagnose queries when it has no package hint. Nobody
// owns it, so /dev/query answers 「not uploaded」 (result 0, no packageInfo) —
// but only after it has checked the SIG, which is the part that matters.
const probePackage = "tech.baici.apkgo.doctorprobe"

// diagnose is registered with `apkgo doctor`. It runs two layered probes:
//
//  1. cert  — public-key certificate is loadable, RSA, and not expired
//  2. query — /dev/query accepts the SIG (proves email + private_key + cert
//     all line up) and returns the stored package info if any
//
// cert never leaves the process, so probe 2 is the only one that checks the
// email / private_key pair. It always runs: without a package hint it queries
// probePackage. (It used to be skipped, which let a wrong private key pass as
// verified on cert alone and fail only at publish time with -20014 密码错误.)
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	s, err := New(cfg)
	if err != nil {
		return []store.Probe{{Name: "cert", Status: "fail", Error: err.Error()}}
	}
	return s.diagnose(ctx, hint)
}

func (s *Store) diagnose(ctx context.Context, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 2)
	probes = append(probes, store.Probe{Name: "cert", Status: "ok", Detail: fmt.Sprintf("RSA-%d public key loaded", s.pubKey.N.BitLen())})

	pkg := hint.Package
	if pkg == "" {
		pkg = probePackage
	}
	info, err := s.query(ctx, pkg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "query", Status: "fail", Error: err.Error()})
		return probes
	}
	if hint.Package == "" {
		probes = append(probes, store.Probe{Name: "query", Status: "ok", Detail: "email + private_key accepted (pass --package or --file to also look up an app)"})
		return probes
	}
	detail := fmt.Sprintf("%s not yet uploaded under this account", hint.Package)
	if info != nil {
		detail = fmt.Sprintf("%s → versionCode=%d versionName=%s", info.PackageName, info.VersionCode, info.VersionName)
	}
	probes = append(probes, store.Probe{Name: "query", Status: "ok", Detail: detail})
	return probes
}
