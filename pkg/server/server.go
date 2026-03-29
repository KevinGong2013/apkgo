package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KevinGong2013/apkgo/pkg/apk"
	"github.com/KevinGong2013/apkgo/pkg/config"
	"github.com/KevinGong2013/apkgo/pkg/store"
	"github.com/KevinGong2013/apkgo/pkg/uploader"
)

//go:embed static/index.html
var indexHTML []byte

// Server serves the web GUI and upload API.
type Server struct {
	Config  *config.Config
	Timeout time.Duration
}

// Start begins listening on the given port.
func (s *Server) Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/stores", s.handleStores)
	mux.HandleFunc("POST /api/upload", s.handleUpload)

	addr := fmt.Sprintf(":%d", port)
	slog.Info("starting server", "addr", fmt.Sprintf("http://localhost%s", addr))
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func (s *Server) handleStores(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(s.Config.Stores))
	for name := range s.Config.Stores {
		names = append(names, name)
	}
	sort.Strings(names)
	writeJSON(w, http.StatusOK, map[string]any{"stores": names})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart (512MB max)
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "parse form: " + err.Error()})
		return
	}

	// Save primary APK to temp
	apkPath, err := saveFormFile(r, "file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file: " + err.Error()})
		return
	}
	defer os.Remove(apkPath)

	// Optional 64-bit APK
	var apk64Path string
	if _, _, err := r.FormFile("file64"); err == nil {
		apk64Path, err = saveFormFile(r, "file64")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file64: " + err.Error()})
			return
		}
		defer os.Remove(apk64Path)
	}

	// Parse APK metadata
	info, err := apk.Parse(apkPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "parse apk: " + err.Error()})
		return
	}

	// Filter stores
	var filter []string
	if stores := r.FormValue("stores"); stores != "" {
		filter = strings.Split(stores, ",")
	}

	stores, err := s.Config.CreateStores(filter)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Build request
	req := &store.UploadRequest{
		FilePath:     apkPath,
		File64Path:   apk64Path,
		AppName:      info.AppName,
		PackageName:  info.PackageName,
		VersionCode:  info.VersionCode,
		VersionName:  info.VersionName,
		ReleaseNotes: r.FormValue("notes"),
	}

	// Upload with timeout
	ctx, cancel := context.WithTimeout(r.Context(), s.Timeout)
	defer cancel()

	u := &uploader.Uploader{Stores: stores}
	results := u.Run(ctx, req)

	writeJSON(w, http.StatusOK, map[string]any{
		"apk":     info,
		"results": results,
	})
}

// saveFormFile saves an uploaded file to a temp directory and returns the path.
func saveFormFile(r *http.Request, fieldName string) (string, error) {
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		return "", err
	}
	defer file.Close()

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = ".apk"
	}

	tmp, err := os.CreateTemp("", "apkgo-*"+ext)
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, file); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}

	return tmp.Name(), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
