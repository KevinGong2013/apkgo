package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/pkg/apk"
	"github.com/KevinGong2013/apkgo/pkg/config"
	"github.com/KevinGong2013/apkgo/pkg/history"
	"github.com/KevinGong2013/apkgo/pkg/store"
	"github.com/KevinGong2013/apkgo/pkg/telemetry"
	"github.com/KevinGong2013/apkgo/pkg/uploader"
)

var (
	flagFile      string
	flagFile64    string
	flagStore     string
	flagNotes     string
	flagNotesFile string
	flagDryRun    bool
)

func init() {
	uploadCmd.Flags().StringVarP(&flagFile, "file", "f", "", "APK file path (required)")
	uploadCmd.Flags().StringVar(&flagFile64, "file64", "", "64-bit APK file path (for split-arch uploads)")
	uploadCmd.Flags().StringVarP(&flagStore, "store", "s", "", "comma-separated store names (default: all configured)")
	uploadCmd.Flags().StringVarP(&flagNotes, "notes", "n", "", "release notes (text)")
	uploadCmd.Flags().StringVar(&flagNotesFile, "notes-file", "", "read release notes from file (overrides --notes)")
	uploadCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "validate config and APK without uploading")
	uploadCmd.MarkFlagRequired("file")
	rootCmd.AddCommand(uploadCmd)
}

type uploadOutput struct {
	APK     *apk.Info              `json:"apk"`
	DryRun  bool                   `json:"dry_run,omitempty"`
	Results []*store.UploadResult  `json:"results,omitempty"`
}

var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload APK to configured stores",
	Example: `  apkgo upload -f app.apk
  apkgo upload -f app.apk --store huawei,xiaomi
  apkgo upload -f app.apk --dry-run
  apkgo upload -f app.apk --notes "Bug fixes"
  apkgo upload -f app.apk --notes-file CHANGELOG.md`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate APK file exists
		if _, err := os.Stat(flagFile); err != nil {
			return fmt.Errorf("apk file: %w", err)
		}
		if flagFile64 != "" {
			if _, err := os.Stat(flagFile64); err != nil {
				return fmt.Errorf("64-bit apk file: %w", err)
			}
		}

		// Parse APK metadata
		info, err := apk.Parse(flagFile)
		if err != nil {
			return fmt.Errorf("parse apk: %w", err)
		}
		slog.Info("parsed apk", "package", info.PackageName, "version", info.VersionName, "code", info.VersionCode)

		// Load config
		cfg, err := config.Load(flagConfig)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}

		// Parse store filter
		var filter []string
		if flagStore != "" {
			filter = strings.Split(flagStore, ",")
		}

		// Resolve release notes: --notes-file takes precedence over --notes
		releaseNotes := flagNotes
		if flagNotesFile != "" {
			data, err := os.ReadFile(flagNotesFile)
			if err != nil {
				return fmt.Errorf("notes-file: %w", err)
			}
			releaseNotes = strings.TrimSpace(string(data))
		}

		// Create store instances
		stores, err := cfg.CreateStores(filter)
		if err != nil {
			return err
		}

		// Build upload request
		req := &store.UploadRequest{
			FilePath:     flagFile,
			File64Path:   flagFile64,
			AppName:      info.AppName,
			PackageName:  info.PackageName,
			VersionCode:  info.VersionCode,
			VersionName:  info.VersionName,
			ReleaseNotes: releaseNotes,
		}

		// Dry-run: just output what would happen
		if flagDryRun {
			storeNames := make([]string, len(stores))
			for i, s := range stores {
				storeNames[i] = s.Name()
			}
			writeOutput(uploadOutput{
				APK:    info,
				DryRun: true,
				Results: func() []*store.UploadResult {
					r := make([]*store.UploadResult, len(stores))
					for i, name := range storeNames {
						r[i] = &store.UploadResult{Store: name, Success: true}
					}
					return r
				}(),
			})
			return nil
		}

		// Upload concurrently with timeout
		ctx, cancel := context.WithTimeout(cmd.Context(), flagTimeout)
		defer cancel()

		u := &uploader.Uploader{Stores: stores}
		results := u.Run(ctx, req)

		writeOutput(uploadOutput{APK: info, Results: results})

		// Save to local history
		if err := history.Append(history.DefaultPath(), info, results); err != nil {
			slog.Warn("failed to save history", "error", err)
		}

		// Send anonymous telemetry
		storeResults := make([]telemetry.StoreResult, len(results))
		for i, r := range results {
			storeResults[i] = telemetry.StoreResult{Name: r.Store, Success: r.Success}
		}
		telemetry.Send(telemetry.Event{
			Event:   "upload",
			Source:  "cli",
			Version: Version,
			Stores:  storeResults,
		})

		// Set exit code based on results
		failures := 0
		for _, r := range results {
			if !r.Success {
				failures++
			}
		}
		switch {
		case failures == 0:
			exitCode = 0
		case failures == len(results):
			exitCode = 2
		default:
			exitCode = 1
		}
		return nil
	},
}
