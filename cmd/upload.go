package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"

	"github.com/KevinGong2013/apkgo/v3/pkg/apk"
	"github.com/KevinGong2013/apkgo/v3/pkg/apkgo"
	"github.com/KevinGong2013/apkgo/v3/pkg/history"
	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
	"github.com/KevinGong2013/apkgo/v3/pkg/telemetry"
	"github.com/KevinGong2013/apkgo/v3/pkg/uploader"
)

var (
	flagFile           string
	flagFile64         string
	flagStore          string
	flagNotes          string
	flagNotesFile      string
	flagReleaseTime    string
	flagDryRun         bool
	flagFetchHeaders   []string
	flagProgressStream bool
)

func init() {
	uploadCmd.Flags().StringVarP(&flagFile, "file", "f", "", "APK or AAB file path or http(s) URL (required; .aab is googleplay-only)")
	uploadCmd.Flags().StringVar(&flagFile64, "file64", "", "64-bit APK file path or http(s) URL (for split-arch uploads)")
	uploadCmd.Flags().StringVarP(&flagStore, "store", "s", "", "comma-separated store names (default: all configured)")
	uploadCmd.Flags().StringVarP(&flagNotes, "notes", "n", "", "release notes (text)")
	uploadCmd.Flags().StringVar(&flagNotesFile, "notes-file", "", "read release notes from file (overrides --notes)")
	uploadCmd.Flags().StringVar(&flagReleaseTime, "release-time", "", "schedule a timed release (定时发布) at an RFC3339 time, e.g. 2026-06-20T10:00:00+08:00 (supported: huawei,honor,xiaomi,oppo,vivo,samsung,tencent; others release immediately)")
	uploadCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "validate config and APK without uploading")
	uploadCmd.Flags().StringArrayVar(&flagFetchHeaders, "fetch-header", nil, `extra HTTP header for URL fetches (repeatable; "Name: value")`)
	uploadCmd.Flags().BoolVar(&flagProgressStream, "progress-stream", false, "emit NDJSON progress events on stdout (one JSON object per line) for parent-process consumption")
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
  apkgo upload -f app.apk --notes-file CHANGELOG.md
  apkgo upload -f https://artifacts.example.com/app-v1.apk --store huawei
  apkgo upload -f https://private.example.com/app.apk --fetch-header "Authorization: Bearer xxx"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fetchHeaders, err := httpx.ParseHeaders(flagFetchHeaders)
		if err != nil {
			return err
		}

		var releaseTime time.Time
		if flagReleaseTime != "" {
			releaseTime, err = time.Parse(time.RFC3339, flagReleaseTime)
			if err != nil {
				return fmt.Errorf("invalid --release-time %q (want RFC3339 with timezone, e.g. 2026-06-20T10:00:00+08:00): %w", flagReleaseTime, err)
			}
		}

		cfg, err := loadConfigForCmd()
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}

		// Build the progress manager based on CLI flags. This is CLI-only
		// concern (TTY detection, slog redirection); the library doesn't
		// know or care about it.
		pm, nd := pickProgressManager()

		var stores []string
		if flagStore != "" {
			stores = strings.Split(flagStore, ",")
		}

		result, err := apkgo.Run(cmd.Context(), apkgo.Job{
			APKFile:      flagFile,
			APKFile64:    flagFile64,
			Stores:       stores,
			Notes:        flagNotes,
			NotesFile:    flagNotesFile,
			ReleaseTime:  releaseTime,
			Config:       cfg,
			FetchHeaders: fetchHeaders,
			Progress:     pm,
			Timeout:      flagTimeout,
			DryRun:       flagDryRun,
		})
		if err != nil {
			return err
		}

		// Stream-mode runs already emitted a "done" event via the
		// NDJSONManager. Non-stream mode prints the final aggregate JSON.
		if nd == nil {
			writeOutput(uploadOutput{
				APK:     result.APK,
				DryRun:  result.DryRun,
				Results: result.Results,
			})
		}

		// Persist locally and report anonymous telemetry — both are CLI
		// behaviours; library callers do their own equivalents.
		if !result.DryRun {
			if err := history.Append(history.DefaultPath(), result.APK, result.Results); err != nil {
				slog.Warn("failed to save history", "error", err)
			}
			storeResults := make([]telemetry.StoreResult, len(result.Results))
			for i, r := range result.Results {
				storeResults[i] = telemetry.StoreResult{Name: r.Store, Success: r.Success}
			}
			telemetry.Send(telemetry.Event{
				Event:   "upload",
				Source:  "cli",
				Version: Version,
				Stores:  storeResults,
			})
		}

		// Exit code reflects how many stores failed.
		failures := 0
		for _, r := range result.Results {
			if !r.Success {
				failures++
			}
		}
		switch {
		case failures == 0:
			exitCode = 0
		case failures == len(result.Results):
			exitCode = 2
		default:
			exitCode = 1
		}
		return nil
	},
}

// pickProgressManager builds the progress manager appropriate for the
// CLI's flag combination. Returns the manager plus a non-nil pointer
// when the manager is the NDJSON streamer (so the caller knows to
// suppress the post-run JSON dump).
func pickProgressManager() (uploader.ProgressManager, *uploader.NDJSONManager) {
	switch {
	case flagProgressStream:
		nd := uploader.NewNDJSONManager(os.Stdout)
		return nd, nd
	case isStderrTTY() && !flagVerbose:
		p := mpb.New(
			mpb.WithOutput(os.Stderr),
			mpb.WithWidth(32),
			mpb.WithAutoRefresh(),
		)
		// Route slog through mpb so log lines render above the bars.
		handler := slog.NewTextHandler(p, &slog.HandlerOptions{Level: slog.LevelWarn})
		slog.SetDefault(slog.New(handler))
		return uploader.NewManager(p), nil
	default:
		return uploader.NopManager, nil
	}
}
