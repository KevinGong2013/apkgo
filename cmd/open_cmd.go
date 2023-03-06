package cmd

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/KevinGong2013/apkgo/cmd/publisher"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

var openCommand = &cobra.Command{
	Use:   "open",
	Short: "打开各个平台的管理后台",
	Run:   runOpen,
}

func init() {

	rootCmd.AddCommand(openCommand)
}

func runOpen(cmd *cobra.Command, args []string) {

	if len(args) > 1 {
		fatalErr("只支持打开一个商店的管理后台")
	}

	var store string

	if len(args) == 0 {
		prompt := &survey.Select{
			Message:  "请选择认证信息的存储方式",
			Options:  []string{"huawei", "xiaomi", "oppo", "vivo", "tencent", "baidu", "pyger", "fir", "qh360"},
			PageSize: 9,
		}
		survey.AskOne(prompt, &store)
	} else {
		store = args[0]
	}

	switch store {
	case "oppo", "tencent", "baidu", "qh360":
		p, err := publisher.NewBrowserPublisher(store, browserUserDataDir())
		if err != nil {
			fatalErr(err.Error())
		}
		p.CheckAuth(true)
	default:
		fmt.Println(text.FgRed.Sprintf("%s请直接通过浏览器访问", store))
	}

}
