package cmd

import (
	"fmt"
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

func renderDoctorText(out doctorOutput) {
	for _, r := range out.Stores {
		if !r.Supported {
			fmt.Printf("%s: doctor not implemented\n", r.Store)
			continue
		}
		fmt.Printf("%s:\n", r.Store)
		for _, p := range r.Probes {
			icon := "?"
			switch p.Status {
			case "ok":
				icon = "✓"
			case "fail":
				icon = "✗"
			case "skip":
				icon = "-"
			}
			msg := p.Detail
			if p.Error != "" {
				msg = p.Error
			}
			fmt.Printf("  %s %-20s %s\n", icon, p.Name, msg)
		}
	}
}
