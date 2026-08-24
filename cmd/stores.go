package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/v3/pkg/config"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"

	// Import all store packages to trigger init() registration.
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/fir"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/googleplay"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/honor"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/huawei"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/meizu"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/oppo"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/pgyer"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/samsung"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/script"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/tencent"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/vivo"
	_ "github.com/KevinGong2013/apkgo/v3/pkg/store/xiaomi"
)

var flagStoresConfigured bool

func init() {
	rootCmd.AddCommand(storesCmd)
	storesCmd.Flags().BoolVar(&flagStoresConfigured, "configured", false, "list store names configured in the resolved config")
}

var storesCmd = &cobra.Command{
	Use:   "stores",
	Short: "List supported stores and their configuration schema",
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagStoresConfigured {
			cfg, err := loadConfigForCmd()
			if err != nil {
				return err
			}
			names, err := configuredStoreNames(cfg)
			if err != nil {
				return err
			}
			if flagOutput == "text" {
				for _, name := range names {
					fmt.Println(name)
				}
			} else {
				writeOutput(map[string]any{"stores": names})
			}
			return nil
		}

		schemas := store.Schemas()
		if flagOutput == "text" {
			for _, s := range schemas {
				fmt.Printf("%-10s", s.Name)
				for _, f := range s.Fields {
					req := ""
					if f.Required {
						req = "*"
					}
					fmt.Printf("  %s%s (%s)", f.Key, req, f.Desc)
				}
				fmt.Println()
			}
			return nil
		}
		writeOutput(map[string]any{"stores": schemas})
		return nil
	},
}

func configuredStoreNames(cfg *config.Config) ([]string, error) {
	names := make([]string, 0, len(cfg.Stores))
	seen := make(map[string]struct{}, len(cfg.Stores))
	for configuredName := range cfg.Stores {
		name := strings.ToLower(configuredName)
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate configured store name ignoring case: %s", name)
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
