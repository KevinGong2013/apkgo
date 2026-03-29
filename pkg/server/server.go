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
	"sync"
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
	Config     *config.Config
	ConfigPath string
	Timeout    time.Duration
	mu         sync.RWMutex
}

// Start begins listening on the given port.
func (s *Server) Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/stores", s.handleStores)
	mux.HandleFunc("GET /api/stores/schema", s.handleStoresSchema)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("POST /api/config", s.handleSaveConfig)
	mux.HandleFunc("POST /api/upload", s.handleUpload)

	addr := fmt.Sprintf(":%d", port)
	slog.Info("starting server", "addr", fmt.Sprintf("http://localhost%s", addr))
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

// handleStores returns configured store names.
func (s *Server) handleStores(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.Config.Stores))
	for name := range s.Config.Stores {
		names = append(names, name)
	}
	sort.Strings(names)
	writeJSON(w, http.StatusOK, map[string]any{"stores": names})
}

// handleStoresSchema returns all registered store schemas (available stores + required fields).
func (s *Server) handleStoresSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"stores": store.Schemas()})
}

// handleGetConfig returns the current config (values masked for security).
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return store configs with values masked (show only which keys are set)
	result := map[string]map[string]string{}
	for name, cfg := range s.Config.Stores {
		masked := map[string]string{}
		for k, v := range cfg {
			if v != "" {
				masked[k] = maskValue(v)
			} else {
				masked[k] = ""
			}
		}
		result[name] = masked
	}
	writeJSON(w, http.StatusOK, map[string]any{"stores": result})
}

// handleSaveConfig saves store configuration.
// Body: {"stores": {"huawei": {"client_id": "xxx", ...}, ...}}
func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Stores map[string]map[string]string `json:"stores"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Merge: update provided fields, keep existing ones not in request
	for name, fields := range body.Stores {
		if s.Config.Stores[name] == nil {
			s.Config.Stores[name] = map[string]string{}
		}
		for k, v := range fields {
			s.Config.Stores[name][k] = v
		}
	}

	// Remove stores with all empty values
	for name, fields := range s.Config.Stores {
		allEmpty := true
		for _, v := range fields {
			if v != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			delete(s.Config.Stores, name)
		}
	}

	// Save to file
	if err := s.Config.Save(s.ConfigPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "save: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"saved": s.ConfigPath})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "parse form: " + err.Error()})
		return
	}

	apkPath, err := saveFormFile(r, "file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file: " + err.Error()})
		return
	}
	defer os.Remove(apkPath)

	var apk64Path string
	if _, _, err := r.FormFile("file64"); err == nil {
		apk64Path, err = saveFormFile(r, "file64")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file64: " + err.Error()})
			return
		}
		defer os.Remove(apk64Path)
	}

	info, err := apk.Parse(apkPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "parse apk: " + err.Error()})
		return
	}

	var filter []string
	if stores := r.FormValue("stores"); stores != "" {
		filter = strings.Split(stores, ",")
	}

	s.mu.RLock()
	stores, err := s.Config.CreateStores(filter)
	s.mu.RUnlock()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	req := &store.UploadRequest{
		FilePath:     apkPath,
		File64Path:   apk64Path,
		AppName:      info.AppName,
		PackageName:  info.PackageName,
		VersionCode:  info.VersionCode,
		VersionName:  info.VersionName,
		ReleaseNotes: r.FormValue("notes"),
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.Timeout)
	defer cancel()

	u := &uploader.Uploader{Stores: stores}
	results := u.Run(ctx, req)

	writeJSON(w, http.StatusOK, map[string]any{
		"apk":     info,
		"results": results,
	})
}

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

func maskValue(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return v[:2] + "****" + v[len(v)-2:]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
