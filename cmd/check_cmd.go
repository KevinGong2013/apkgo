package cmd

import (
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/spf13/cobra"
)

var checkCommand = &cobra.Command{
	Use:   "check",
	Short: "预检查各个平台的授权信息 [todo]",
	Run:   runCheck,
}

func init() {

	rootCmd.AddCommand(checkCommand)

	checkCommand.Flags().StringSliceVarP(&stores, "store", "s", []string{}, "需要上传到哪些商店。 [-s all] 上传到配置文件中的所有商店")
}

func runCheck(cmd *cobra.Command, args []string) {

	initialPublishers(false)
	defer browserCancelFunc()

	for _, publisher := range publishers {
		if p, ok := publisher.(shared.Checker); ok {
			p.CheckAuth(true)
		}
	}
}
