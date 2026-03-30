package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/KevinGong2013/apkgo/pkg/history"
)

var flagHistoryLimit int

func init() {
	historyCmd.Flags().IntVarP(&flagHistoryLimit, "limit", "n", 10, "number of recent records to show")
	rootCmd.AddCommand(historyCmd)
}

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Show local upload history",
	Example: `  apkgo history
  apkgo history -n 5
  apkgo history -o text`,
	RunE: func(cmd *cobra.Command, args []string) error {
		records, err := history.Read(history.DefaultPath())
		if err != nil {
			return fmt.Errorf("read history: %w", err)
		}

		if len(records) == 0 {
			if flagOutput == "text" {
				fmt.Println("No upload history.")
				return nil
			}
			writeOutput(map[string]any{"records": []any{}})
			return nil
		}

		// Take last N records
		if flagHistoryLimit > 0 && flagHistoryLimit < len(records) {
			records = records[len(records)-flagHistoryLimit:]
		}

		if flagOutput == "text" {
			for _, r := range records {
				status := "OK"
				for _, res := range r.Results {
					if !res.Success {
						status = "PARTIAL"
						break
					}
				}
				allFailed := true
				for _, res := range r.Results {
					if res.Success {
						allFailed = false
						break
					}
				}
				if allFailed && len(r.Results) > 0 {
					status = "FAIL"
				}

				stores := ""
				for _, res := range r.Results {
					mark := "+"
					if !res.Success {
						mark = "-"
					}
					stores += fmt.Sprintf(" %s%s", mark, res.Store)
				}
				fmt.Printf("%s  %-6s  %s %s (%d)%s\n",
					r.Timestamp[:19], status,
					r.APK.PackageName, r.APK.VersionName, r.APK.VersionCode, stores)
			}
			return nil
		}

		writeOutput(map[string]any{"records": records})
		return nil
	},
}
