package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/pkg/store"
)

var flagInitStore string

func init() {
	initCmd.Flags().StringVarP(&flagInitStore, "store", "s", "", "comma-separated store names (default: all)")
	rootCmd.AddCommand(initCmd)
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a config file",
	Example: `  apkgo init
  apkgo init --store huawei,xiaomi
  apkgo init -c my-config.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if file already exists
		if _, err := os.Stat(flagConfig); err == nil {
			return fmt.Errorf("%s already exists (use -c to specify a different path)", flagConfig)
		}

		// Determine which stores to include
		wanted := map[string]bool{}
		if flagInitStore != "" {
			for _, s := range strings.Split(flagInitStore, ",") {
				wanted[strings.TrimSpace(s)] = true
			}
		}

		schemas := store.Schemas()

		var b strings.Builder
		b.WriteString("# apkgo configuration\n")
		b.WriteString("# Docs: https://github.com/KevinGong2013/apkgo\n\n")
		b.WriteString("stores:\n")

		included := 0
		for _, schema := range schemas {
			if len(wanted) > 0 && !wanted[schema.Name] {
				continue
			}
			included++
			b.WriteString(fmt.Sprintf("  %s:\n", schema.Name))
			for _, f := range schema.Fields {
				req := ""
				if f.Required {
					req = " (required)"
				}
				b.WriteString(fmt.Sprintf("    # %s%s\n", f.Desc, req))
				b.WriteString(fmt.Sprintf("    %s: \"\"\n", f.Key))
			}
			b.WriteString("\n")
		}

		b.WriteString("# Update check interval: 30d (default), 7d, 0 to disable\n")
		b.WriteString("# update_check: 30d\n")

		if included == 0 {
			return fmt.Errorf("no matching stores found; available: %s", strings.Join(store.Names(), ", "))
		}

		if err := os.WriteFile(flagConfig, []byte(b.String()), 0644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}

		slog.Info("config created", "path", flagConfig)
		writeOutput(map[string]string{
			"created": flagConfig,
			"stores":  fmt.Sprintf("%d", included),
		})
		return nil
	},
}
