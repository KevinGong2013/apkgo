package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/pkg/apk"
	"github.com/KevinGong2013/apkgo/pkg/config"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

var (
	flagDoctorStore   string
	flagDoctorFile    string
	flagDoctorPackage string
)

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().StringVarP(&flagDoctorStore, "store", "s", "", "comma-separated store names (default: all configured)")
	doctorCmd.Flags().StringVarP(&flagDoctorFile, "file", "f", "", "APK file (used to derive package name for deeper probes)")
	doctorCmd.Flags().StringVarP(&flagDoctorPackage, "package", "p", "", "package name (overrides --file)")
}

type doctorStoreReport struct {
	Store     string        `json:"store"`
	Supported bool          `json:"supported"`
	Probes    []store.Probe `json:"probes,omitempty"`
}

type doctorOutput struct {
	Package string              `json:"package,omitempty"`
	Stores  []doctorStoreReport `json:"stores"`
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose store credentials and permissions",
	Long: `Run readiness probes against configured stores without uploading anything.

For Huawei this validates:
  1. token              — OAuth client credentials are accepted
  2. appid-list         — package name resolves to an appId (needs -f/-p)
  3. release-permission — App release scope is granted (needs -f/-p)

Pass -f <apk> or -p <package> to enable probes that need a real app.`,
	Example: `  apkgo doctor
  apkgo doctor -s huawei
  apkgo doctor -s huawei -f app.apk
  apkgo doctor -s huawei -p com.example.app`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagConfig)
		if err != nil {
			return err
		}

		var filter map[string]bool
		if flagDoctorStore != "" {
			filter = map[string]bool{}
			for _, n := range strings.Split(flagDoctorStore, ",") {
				if n = strings.TrimSpace(n); n != "" {
					filter[n] = true
				}
			}
		}

		pkg := flagDoctorPackage
		if pkg == "" && flagDoctorFile != "" {
			info, err := apk.Parse(flagDoctorFile)
			if err != nil {
				return fmt.Errorf("parse apk: %w", err)
			}
			pkg = info.PackageName
		}
		hint := store.DiagnoseHint{Package: pkg}

		names := make([]string, 0, len(cfg.Stores))
		for name := range cfg.Stores {
			if filter != nil && !filter[name] {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)

		if len(names) == 0 {
			return fmt.Errorf("no matching stores configured")
		}

		out := doctorOutput{Package: pkg}
		anyFail := false

		for _, name := range names {
			scfg := cleanStoreCfg(cfg.Stores[name])
			// Resolve store key for "type.instance" naming (e.g. script.cdn).
			key := name
			if dot := strings.Index(name, "."); dot > 0 {
				key = name[:dot]
			}
			probes, supported := store.Diagnose(cmd.Context(), key, scfg, hint)
			out.Stores = append(out.Stores, doctorStoreReport{Store: name, Supported: supported, Probes: probes})
			for _, p := range probes {
				if p.Status == "fail" {
					anyFail = true
				}
			}
		}

		if flagOutput == "text" {
			renderDoctorText(out)
		} else {
			writeOutput(out)
		}

		if anyFail {
			exitCode = 1
		}
		return nil
	},
}

// cleanStoreCfg strips hook keys so they are not handed to a diagnoser
// as if they were credentials.
func cleanStoreCfg(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if k == "before" || k == "after" {
			continue
		}
		out[k] = v
	}
	return out
}

// renderDoctorText prints a compact summary by default and an
// expanded per-probe view under -v (flagVerbose).
//
// Default: one line per store, sorted with errors first so the most
// urgent thing is at the top of the screen. Trailing summary line
// counts ready / needing-package / errored stores.
func renderDoctorText(out doctorOutput) {
	tty := stdoutIsTTY()

	type row struct {
		store    string
		bucket   string // "fail" | "skip" | "ready" | "unsupported"
		report   doctorStoreReport
		oneLiner string
	}
	rows := make([]row, 0, len(out.Stores))
	for _, r := range out.Stores {
		rows = append(rows, row{
			store:    r.Store,
			bucket:   storeBucket(r),
			report:   r,
			oneLiner: storeOneLiner(r),
		})
	}

	// Sort: failures first, then skip-only, then ready, then unsupported.
	bucketOrder := map[string]int{"fail": 0, "skip": 1, "ready": 2, "unsupported": 3}
	sort.SliceStable(rows, func(i, j int) bool {
		bi, bj := bucketOrder[rows[i].bucket], bucketOrder[rows[j].bucket]
		if bi != bj {
			return bi < bj
		}
		return rows[i].store < rows[j].store
	})

	var ready, needPkg, errored, unsupported int
	for _, r := range rows {
		switch r.bucket {
		case "fail":
			errored++
		case "skip":
			needPkg++
		case "ready":
			ready++
		case "unsupported":
			unsupported++
		}
	}

	// Default (compact) output
	if !flagVerbose {
		nameWidth := 8 // min
		for _, r := range rows {
			if len(r.store) > nameWidth {
				nameWidth = len(r.store)
			}
		}
		for _, r := range rows {
			color := bucketColor(r.bucket)
			fmt.Printf("%s%-*s%s  %s\n", color.on(tty), nameWidth, r.store, color.off(tty), r.oneLiner)
		}
		fmt.Println()
		printSummary(ready, needPkg, errored, unsupported, len(rows), tty)
		return
	}

	// Verbose: full per-probe breakdown, one block per store
	for _, r := range rows {
		color := bucketColor(r.bucket)
		fmt.Printf("%s%s%s:\n", color.on(tty), r.report.Store, color.off(tty))
		if !r.report.Supported {
			fmt.Println("  doctor not implemented")
			continue
		}
		for _, p := range r.report.Probes {
			icon := probeIcon(p.Status)
			msg := p.Detail
			if p.Error != "" {
				msg = p.Error
			}
			fmt.Printf("  %s %-20s %s\n", icon, p.Name, msg)
			if p.VerboseDetail != "" {
				fmt.Printf("      %-18s %s\n", "", p.VerboseDetail)
			}
		}
	}
	fmt.Println()
	printSummary(ready, needPkg, errored, unsupported, len(rows), tty)
}

// storeBucket classifies a store's overall health for sorting and
// summary counting.
//
// Philosophy: a store with at least one passing probe is "ready" —
// the credentials work and the operator's basic concern (is this
// configured?) is answered. Skipped probes mean "this check could
// have run with --package" but are not a problem on their own; pass
// --package or use -v to see the full breakdown.
func storeBucket(r doctorStoreReport) string {
	if !r.Supported {
		return "unsupported"
	}
	hasFail, hasOK, hasSkip := false, false, false
	for _, p := range r.Probes {
		switch p.Status {
		case "fail":
			hasFail = true
		case "ok":
			hasOK = true
		case "skip":
			hasSkip = true
		}
	}
	switch {
	case hasFail:
		return "fail"
	case hasOK:
		return "ready"
	case hasSkip:
		return "skip"
	default:
		return "ready"
	}
}

// storeOneLiner produces the compact single-line description.
//
//   ready: ✅ ready
//   fail:  ❌ <probe>: <error>
//   skip:    (blank) needs --package for full check
//   unsup:   (blank) doctor not implemented
func storeOneLiner(r doctorStoreReport) string {
	if !r.Supported {
		return "   doctor not implemented"
	}
	for _, p := range r.Probes {
		if p.Status == "fail" {
			msg := p.Error
			if msg == "" {
				msg = p.Detail
			}
			return fmt.Sprintf("❌ %s: %s", p.Name, msg)
		}
	}
	for _, p := range r.Probes {
		if p.Status == "ok" {
			return "✅ ready"
		}
	}
	// All skipped (no auth-only probe to run without --package).
	return "   needs --package for full check"
}

// probeIcon picks the per-probe glyph for verbose mode.
func probeIcon(status string) string {
	switch status {
	case "ok":
		return "✅"
	case "fail":
		return "❌"
	case "skip":
		return "  "
	default:
		return "  "
	}
}

// printSummary writes the trailing one-line tally.
func printSummary(ready, needPkg, errored, unsupported, total int, tty bool) {
	parts := []string{
		fmt.Sprintf("%d/%d ready", ready, total),
	}
	if needPkg > 0 {
		parts = append(parts, fmt.Sprintf("%d need --package", needPkg))
	}
	parts = append(parts, fmt.Sprintf("%d errors", errored))
	if unsupported > 0 {
		parts = append(parts, fmt.Sprintf("%d not implemented", unsupported))
	}
	c := summaryColor(errored, needPkg)
	fmt.Printf("%s%s%s\n", c.on(tty), strings.Join(parts, ", "), c.off(tty))
}

// --- color + tty helpers ---

type ansiColor struct{ code string }

func (c ansiColor) on(tty bool) string {
	if !tty || c.code == "" {
		return ""
	}
	return "\x1b[" + c.code + "m"
}
func (c ansiColor) off(tty bool) string {
	if !tty || c.code == "" {
		return ""
	}
	return "\x1b[0m"
}

func bucketColor(bucket string) ansiColor {
	switch bucket {
	case "fail":
		return ansiColor{"31"} // red
	case "skip":
		return ansiColor{"33"} // yellow
	case "ready":
		return ansiColor{"32"} // green
	default:
		return ansiColor{}
	}
}

func summaryColor(errored, needPkg int) ansiColor {
	switch {
	case errored > 0:
		return ansiColor{"31"}
	case needPkg > 0:
		return ansiColor{"33"}
	default:
		return ansiColor{"32"}
	}
}

// stdoutIsTTY reports whether stdout looks interactive enough for ANSI
// colour codes to be useful. Falls back to "no" for any uncertainty
// (e.g. piped output, redirected file) — colour leaking into a log file
// is more annoying than missing colour in a terminal.
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
