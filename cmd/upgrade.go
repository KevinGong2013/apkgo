package cmd

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

const releaseAPI = "https://api.github.com/repos/KevinGong2013/apkgo/releases/latest"

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade apkgo to the latest version",
	Example: `  apkgo upgrade
  apkgo upgrade --dry-run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Info("checking latest version...")

		// 1. Get latest release info
		release, err := fetchLatestRelease()
		if err != nil {
			return fmt.Errorf("check update: %w", err)
		}

		latest := strings.TrimPrefix(release.TagName, "v")
		current := strings.TrimPrefix(Version, "v")

		if latest == current {
			writeOutput(map[string]string{
				"current": Version,
				"latest":  release.TagName,
				"status":  "up_to_date",
			})
			return nil
		}

		slog.Info("new version available", "current", Version, "latest", release.TagName)

		if flagDryRun {
			writeOutput(map[string]string{
				"current":  Version,
				"latest":   release.TagName,
				"status":   "update_available",
				"download": findAssetURL(release),
			})
			return nil
		}

		// 2. Find the right asset for this platform
		assetURL := findAssetURL(release)
		if assetURL == "" {
			return fmt.Errorf("no binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, release.TagName)
		}

		slog.Info("downloading", "url", assetURL)

		// 3. Download and extract
		binary, err := downloadAndExtract(assetURL)
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}

		// 4. Replace current binary
		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("find executable: %w", err)
		}

		if err := replaceBinary(execPath, binary); err != nil {
			return fmt.Errorf("replace binary: %w", err)
		}

		writeOutput(map[string]string{
			"current": Version,
			"latest":  release.TagName,
			"status":  "upgraded",
			"path":    execPath,
		})
		return nil
	},
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func fetchLatestRelease() (*ghRelease, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(releaseAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func findAssetURL(release *ghRelease) string {
	goOS := strings.Title(runtime.GOOS)
	goArch := runtime.GOARCH
	if goArch == "amd64" {
		goArch = "x86_64"
	}

	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, goOS) && strings.Contains(asset.Name, goArch) {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func downloadAndExtract(assetURL string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(assetURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if strings.HasSuffix(assetURL, ".tar.gz") {
		return extractTarGz(data)
	}
	if strings.HasSuffix(assetURL, ".zip") {
		return extractZip(data)
	}
	return nil, fmt.Errorf("unknown archive format: %s", assetURL)
}

func extractTarGz(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(hdr.Name, "apkgo") && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("apkgo binary not found in archive")
}

func extractZip(data []byte) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if strings.HasSuffix(f.Name, "apkgo") || strings.HasSuffix(f.Name, "apkgo.exe") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("apkgo binary not found in archive")
}

func replaceBinary(path string, newBinary []byte) error {
	// Write to temp file first, then atomic rename
	tmp := path + ".new"
	if err := os.WriteFile(tmp, newBinary, 0755); err != nil {
		return err
	}

	// Backup old binary
	backup := path + ".old"
	os.Remove(backup)
	if err := os.Rename(path, backup); err != nil {
		os.Remove(tmp)
		return err
	}

	if err := os.Rename(tmp, path); err != nil {
		// Restore backup
		os.Rename(backup, path)
		return err
	}

	os.Remove(backup)
	return nil
}
