package fir

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

func init() {
	store.Register("fir", store.ConfigSchema{
		Name:       "fir",
		ConsoleURL: "https://www.betaqr.com.cn/docs",
		Fields: []store.FieldSchema{
			{Key: "api_token", Required: true, Desc: "fir.im API token (从控制台 → 账号 → API Token 获取)"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("fir", diagnose)
}

// firErr covers the two error shapes fir.im uses across endpoints:
//
//   /user (auth failures):
//     {"errors":{"exception":["Authentication failed"]},"code":100020}
//
//   /apps (account-state failures, e.g. not real-name verified):
//     {"msg":"没有实名认证不能上传app"}
//
// parseFirErr tries the structured shape first and falls back to msg /
// raw body so callers always print something readable instead of a
// JSON literal.
type firErr struct {
	Errors struct {
		Exception []string `json:"exception"`
	} `json:"errors"`
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func parseFirErr(body []byte) string {
	var e firErr
	if json.Unmarshal(body, &e) == nil {
		if len(e.Errors.Exception) > 0 {
			return fmt.Sprintf("[%d] %s", e.Code, strings.Join(e.Errors.Exception, "; "))
		}
		if e.Msg != "" {
			return e.Msg
		}
	}
	return truncateBody(string(body))
}

// truncateBody caps a response body at 500 chars so diagnostic errors stay readable.
func truncateBody(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

type Store struct {
	client   *resty.Client
	apiToken string
}

func New(cfg map[string]string) (*Store, error) {
	apiToken := strings.TrimSpace(cfg["api_token"])
	if apiToken == "" {
		return nil, fmt.Errorf("api_token is required")
	}

	// Switched to https — fir.im's API host enforces TLS now and the
	// previous http base would silently get redirected.
	client := resty.New().
		SetBaseURL("https://api.bq04.com")

	return &Store{client: client, apiToken: apiToken}, nil
}

func (s *Store) Name() string { return "fir" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(_ context.Context, req *store.UploadRequest) error {
	rep := progress.Safe(req.Progress)

	// 1. Get upload token
	rep.Phase("auth")
	var tokenResp struct {
		ID   string `json:"id"`
		Cert struct {
			Binary struct {
				Key       string `json:"key"`
				Token     string `json:"token"`
				UploadURL string `json:"upload_url"`
			} `json:"binary"`
		} `json:"cert"`
	}

	httpResp, err := s.client.R().
		SetFormData(map[string]string{
			"type":      "android",
			"bundle_id": req.PackageName,
			"api_token": s.apiToken,
		}).
		SetResult(&tokenResp).
		Post("/apps")
	if err != nil {
		return fmt.Errorf("get upload token: %w", err)
	}
	if httpResp.IsError() {
		return fmt.Errorf("get upload token: http %d: %s",
			httpResp.StatusCode(), parseFirErr(httpResp.Body()))
	}
	if tokenResp.ID == "" {
		return fmt.Errorf("get upload token: %s", parseFirErr(httpResp.Body()))
	}

	// 2. Upload binary
	rep.Phase("uploading")
	rc, size, err := progress.OpenFile(req.FilePath, rep)
	if err != nil {
		return fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	var uploadResp struct {
		Completed bool `json:"is_completed"`
	}

	resp, err := httpx.DoMultipart(context.Background(), httpx.MultipartRequest{
		Method: http.MethodPost,
		URL:    tokenResp.Cert.Binary.UploadURL,
		Fields: map[string]string{
			"key":         tokenResp.Cert.Binary.Key,
			"token":       tokenResp.Cert.Binary.Token,
			"x:name":      req.AppName,
			"x:version":   req.VersionName,
			"x:build":     strconv.Itoa(int(req.VersionCode)),
			"x:changelog": req.ReleaseNotes,
		},
		Files: []httpx.FileField{{Field: "file", FileName: filepath.Base(req.FilePath), Reader: rc, Size: size}},
	})
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("upload: http %d: %s", resp.StatusCode, truncateBody(string(body)))
	}
	if jerr := json.Unmarshal(body, &uploadResp); jerr != nil {
		return fmt.Errorf("decode upload response (HTTP %d): %v: %s",
			resp.StatusCode, jerr, truncateBody(string(body)))
	}
	if !uploadResp.Completed {
		return fmt.Errorf("upload incomplete: %s", truncateBody(string(body)))
	}
	return nil
}

// diagnose is registered with `apkgo doctor`. Single probe:
//
//   user — GET /user with the api_token validates the credential
//          without creating an app shell on fir's side. Reports the
//          account name + email on success.
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 1)

	s, err := New(cfg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "config", Status: "fail", Error: err.Error()})
		return probes
	}

	var resp struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	httpResp, err := s.client.R().
		SetQueryParam("api_token", s.apiToken).
		SetResult(&resp).
		Get("/user")
	if err != nil {
		probes = append(probes, store.Probe{Name: "user", Status: "fail", Error: err.Error()})
		return probes
	}
	if httpResp.IsError() {
		probes = append(probes, store.Probe{Name: "user", Status: "fail",
			Error: parseFirErr(httpResp.Body())})
		return probes
	}
	if resp.Email == "" && resp.Name == "" {
		probes = append(probes, store.Probe{Name: "user", Status: "fail",
			Error: fmt.Sprintf("empty user response: %s", truncateBody(httpResp.String()))})
		return probes
	}
	probes = append(probes, store.Probe{
		Name:          "user",
		Status:        "ok",
		Detail:        "account active",
		VerboseDetail: fmt.Sprintf("name=%s email=%s", store.MaskName(resp.Name), store.MaskEmail(resp.Email)),
	})
	return probes
}
