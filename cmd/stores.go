package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/pkg/store"

	// Import all store packages to trigger init() registration.
	_ "github.com/KevinGong2013/apkgo/pkg/store/custom"
	_ "github.com/KevinGong2013/apkgo/pkg/store/honor"
	_ "github.com/KevinGong2013/apkgo/pkg/store/huawei"
	_ "github.com/KevinGong2013/apkgo/pkg/store/oppo"
	_ "github.com/KevinGong2013/apkgo/pkg/store/vivo"
	_ "github.com/KevinGong2013/apkgo/pkg/store/xiaomi"
)

func init() {
	rootCmd.AddCommand(storesCmd)
}

var storesCmd = &cobra.Command{
	Use:   "stores",
	Short: "List supported stores and their configuration schema",
	Run: func(cmd *cobra.Command, args []string) {
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
			return
		}
		writeOutput(map[string]any{"stores": schemas})
	},
}
