package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/v3/pkg/apkgo"
)

var (
	flagAuditStore    string
	flagAuditFile     string
	flagAuditPackage  string
	flagAuditWatch    bool
	flagAuditInterval time.Duration
)

func init() {
	rootCmd.AddCommand(auditCmd)
	auditCmd.Flags().StringVarP(&flagAuditStore, "store", "s", "", "comma-separated store names (default: all configured)")
	auditCmd.Flags().StringVarP(&flagAuditFile, "file", "f", "", "APK file (used to derive package name)")
	auditCmd.Flags().StringVarP(&flagAuditPackage, "package", "p", "", "package name (overrides --file)")
	auditCmd.Flags().BoolVar(&flagAuditWatch, "watch", false, "poll until every store's review resolves (or the global --timeout)")
	auditCmd.Flags().DurationVar(&flagAuditInterval, "interval", 30*time.Second, "poll interval for --watch")
}

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Query store review (审核) status for a submitted version",
	Long: `Look up the current review status of an already-submitted app on each
configured store — decoupled from upload. Upload now finishes at
"submitted (审核中)"; this command polls how the review is going.

Pass -f <apk> or -p <package>. With --watch it polls every --interval
until every store reaches a terminal state (approved / rejected /
withdrawn) or the global --timeout elapses.`,
	Example: `  apkgo audit -p com.example.app
  apkgo audit -f app.apk -s tencent,huawei
  apkgo audit -p com.example.app --watch --interval 1m -t 1h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfigForCmd()
		if err != nil {
			return err
		}
		var stores []string
		if flagAuditStore != "" {
			stores = strings.Split(flagAuditStore, ",")
		}
		job := apkgo.AuditJob{
			Config:  cfg,
			Stores:  stores,
			Package: flagAuditPackage,
			APKFile: flagAuditFile,
		}

		// Single-shot: one query, structured result on stdout.
		if !flagAuditWatch {
			result, err := apkgo.QueryAudit(cmd.Context(), job)
			if err != nil {
				return err
			}
			emitAudit(result)
			if !result.AllResolved() {
				exitCode = 1 // still reviewing — useful for scripts
			}
			return nil
		}

		// --watch: poll on an independent loop until every store resolves
		// or ctx (the global --timeout) is done. Intermediate rounds print
		// a compact line to stderr so stdout stays a single final JSON.
		for {
			result, err := apkgo.QueryAudit(cmd.Context(), job)
			if err != nil {
				return err
			}
			if result.AllResolved() {
				emitAudit(result)
				return nil
			}
			renderAuditProgress(result)
			select {
			case <-cmd.Context().Done():
				emitAudit(result) // timed out — emit what we have
				exitCode = 1
				return nil
			case <-time.After(flagAuditInterval):
			}
		}
	},
}

// emitAudit writes the final structured result to stdout (json by
// default, or a compact text table under -o text).
func emitAudit(result *apkgo.AuditReport) {
	if flagOutput == "text" {
		renderAuditText(result)
		return
	}
	writeOutput(result)
}

func renderAuditText(result *apkgo.AuditReport) {
	tty := stdoutIsTTY()
	rows := append([]apkgo.AuditStoreResult(nil), result.Stores...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Store < rows[j].Store })
	width := 8
	for _, r := range rows {
		if len(r.Store) > width {
			width = len(r.Store)
		}
	}
	for _, r := range rows {
		fmt.Printf("%-*s  %s\n", width, r.Store, auditOneLiner(r, tty))
	}
}

// renderAuditProgress prints an interim, stderr-only line per --watch round
// so a human can follow along without corrupting the stdout JSON.
func renderAuditProgress(result *apkgo.AuditReport) {
	parts := make([]string, 0, len(result.Stores))
	for _, r := range result.Stores {
		if !r.Supported {
			continue
		}
		s := string(r.State)
		if r.Error != "" {
			s = "error"
		}
		parts = append(parts, r.Store+"="+s)
	}
	sort.Strings(parts)
	fmt.Fprintf(os.Stderr, "[audit] %s\n", strings.Join(parts, " "))
}

func auditOneLiner(r apkgo.AuditStoreResult, tty bool) string {
	if !r.Supported {
		return "   audit not supported"
	}
	if r.Error != "" {
		return colorize("31", tty, "⚠ "+r.Error)
	}
	icon, code := auditGlyph(string(r.State))
	line := icon + " " + string(r.State)
	if r.Detail != "" {
		line += " (" + r.Detail + ")"
	}
	return colorize(code, tty, line)
}

// auditGlyph maps a unified AuditState string to an icon + ANSI colour.
func auditGlyph(state string) (icon, color string) {
	switch state {
	case "approved":
		return "✅", "32"
	case "rejected":
		return "❌", "31"
	case "withdrawn":
		return "↩", "33"
	case "reviewing":
		return "⏳", "33"
	default:
		return "•", ""
	}
}

func colorize(code string, tty bool, s string) string {
	if !tty || code == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
