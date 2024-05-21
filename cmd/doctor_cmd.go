package cmd

import (
	"fmt"
	"os"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "检查apkgo环境",
	Run: func(cmd *cobra.Command, args []string) {
		// 检查apkgo环境
		err := CheckApkgoEnv()
		if err != nil {
			fmt.Println(text.FgRed.Sprint(err.Error()))
			os.Exit(1)
		}
		fmt.Println(text.FgGreen.Sprint("环境检查通过，配置正确"))
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func CheckApkgoEnv() error {
	// 检查apkgo环境

	// 检查apkgo是否正确初始化
	sc, err := ParseStoreSecretFile(nil)
	if err != nil {
		return fmt.Errorf("应用商店密钥配置文件%s配置不正确. %s", secretsFile, text.FgRed.Sprint(err.Error()))
	}

	curls, _, plugins, err := InitPublishers(sc)

	if err != nil {
		return fmt.Errorf("初始化发布器失败. %s", text.FgRed.Sprint(err.Error()))
	}

	if len(curls) == 0 && len(plugins) == 0 {
		return fmt.Errorf("未找到任何发布器. %s", text.FgRed.Sprint("请检查是否正确配置 store.json"))
	}

	notifiers := ""
	if sc.Notifiers.Lark != nil {
		notifiers += "lark "
	}
	if sc.Notifiers.Dingtalk != nil {
		notifiers += "dingtalk "
	}
	if sc.Notifiers.Wecom != nil {
		notifiers += "wecom "
	}
	if sc.Notifiers.Webhook != nil {
		notifiers += "webhook "
	}

	fmt.Println(text.FgHiYellow.Sprintf("publishers: curls(%d) plugins(%d)\nnotifiers : %s", len(curls), len(plugins), notifiers))

	return nil
}
