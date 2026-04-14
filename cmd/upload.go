package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"

	"github.com/KevinGong2013/apkgo/pkg/apk"
	"github.com/KevinGong2013/apkgo/pkg/config"
	"github.com/KevinGong2013/apkgo/pkg/history"
	"github.com/KevinGong2013/apkgo/pkg/hooks"
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
	APK     *apk.Info             `json:"apk"`
	DryRun  bool                  `json:"dry_run,omitempty"`
	Results []*store.UploadResult `json:"results,omitempty"`
}

// isStderrTTY reports whether stderr is attached to a real terminal.
// Used to decide whether to render mpb progress bars (which rely on ANSI
// cursor control codes that would corrupt piped or redirected output).
func isStderrTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
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
		storesWithHooks, err := cfg.CreateStores(filter)
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

		// Collect store names
		storeNames := make([]string, len(storesWithHooks))
		entries := make([]uploader.StoreEntry, len(storesWithHooks))
		for i, swh := range storesWithHooks {
			storeNames[i] = swh.Store.Name()
			entries[i] = uploader.StoreEntry{Store: swh.Store, Before: swh.Before, After: swh.After}
		}

		// Dry-run: just output what would happen
		if flagDryRun {
			writeOutput(uploadOutput{
				APK:    info,
				DryRun: true,
				Results: func() []*store.UploadResult {
					r := make([]*store.UploadResult, len(storeNames))
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

		hookEnv := map[string]string{
			"APKGO_PACKAGE": info.PackageName,
			"APKGO_VERSION": info.VersionName,
		}

		// Global before hook
		if cfg.Hooks.Before != "" {
			slog.Info("running global before hook")
			payload := hooks.BeforeAllPayload{FilePath: flagFile, APK: info, Stores: storeNames}
			if err := hooks.RunHook(ctx, cfg.Hooks.Before, payload, hookEnv); err != nil {
				return fmt.Errorf("global before hook: %w", err)
			}
		}

		// Set up multi-bar progress output (stderr). Skip when stderr is not a
		// terminal (piped/redirected) or when verbose logging is on — slog lines
		// and bars would fight for the same lines.
		//
		// WithAutoRefresh is required: mpb's own terminal detection disables
		// auto-refresh for non-stdout writers, but we've already verified the
		// stderr fd is a tty via isStderrTTY() so we force refreshing on.
		var pm *uploader.Manager
		if isStderrTTY() && !flagVerbose {
			p := mpb.New(
				mpb.WithOutput(os.Stderr),
				mpb.WithWidth(32),
				mpb.WithAutoRefresh(),
			)
			pm = uploader.NewManager(p)
			// Route slog through mpb so log lines render above the bars.
			handler := slog.NewTextHandler(p, &slog.HandlerOptions{Level: slog.LevelWarn})
			slog.SetDefault(slog.New(handler))
		}

		u := &uploader.Uploader{Stores: entries, Progress: pm}
		results := u.Run(ctx, req, info)
		pm.Wait()

		// Global after hook
		if cfg.Hooks.After != "" {
			slog.Info("running global after hook")
			payload := hooks.AfterAllPayload{FilePath: flagFile, APK: info, Results: results}
			if err := hooks.RunHook(ctx, cfg.Hooks.After, payload, hookEnv); err != nil {
				slog.Warn("global after hook failed", "error", err)
			}
		}

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
