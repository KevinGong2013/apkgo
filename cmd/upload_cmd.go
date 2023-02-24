/*
Copyright © 2023 Kevin Gong <aoxianglele@icloud.com>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/KevinGong2013/apkgo/cmd/notifiers"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/shogo82148/androidbinary/apk"
	"github.com/spf13/cobra"
)

// uploadCmd represents the upload command
var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "上传apk到指定应用商店",
	Args: func(cmd *cobra.Command, args []string) error {
		// 确认apk文件不能为空且
		if len(file) == 0 && len(file32) == 0 && len(file64) == 0 {
			return errors.New("待上传apk文件不能为空")
		}

		// 确认apk文件合法
		for _, f := range []string{file, file32, file64} {
			if len(f) > 0 {
				if err := validateApkFile(f); err != nil {
					return fmt.Errorf("%s: %s", f, err.Error())
				}
			}
		}

		if len(stores) == 1 && stores[0] == "all" {
			stores = []string{}
			for k := range config.Publishers {
				stores = append(stores, k)
			}
		}

		// 确认想要上传的store 在配置文件中都存在
		for _, s := range stores {
			if config.Publishers[s] == nil {
				return fmt.Errorf("不支持的应用商店. 请检查(%s)是否配置了此商店(%s)授权信息", cfgFilePath, s)
			}
		}

		return nil
	},
	Run: runUpload,
}

var stores []string

var file string
var file32 string
var file64 string

var releaseNots string

var disableDoubleCheck bool

var config Config

type Notifiers struct {
	Lark     *notifiers.LarkNotifier     `json:"lark,omitempty"`
	DingTalk *notifiers.DingTalkNotifier `json:"dingtalk,omitempty"`
	WeCom    *notifiers.WeComNotifier    `json:"wecom,omitempty"`
	WebHook  *notifiers.Webhook          `json:"webhook,omitempty"`
}

type Config struct {
	Publishers map[string]map[string]string `json:"stores"`
	Notifiers  Notifiers                    `json:"notifiers,omitempty"`
}

var cfgFilePath string

func init() {

	rootCmd.AddCommand(uploadCmd)

	// 配置文件
	uploadCmd.Flags().StringVarP(&cfgFilePath, "config", "c", "", "config file (default is $HOME/.apkgo.json)")

	// 需要上传到哪些商店
	uploadCmd.Flags().StringSliceVarP(&stores, "store", "s", []string{}, "需要上传到哪些商店。 [-s all] 上传到配置文件中的所有商店")
	uploadCmd.MarkFlagRequired("store")

	// apk 文件
	uploadCmd.Flags().StringVarP(&file, "file", "f", "", "单包apk文件路径")

	uploadCmd.Flags().StringVarP(&file32, "file32", "", "", "32位apk文件路径 注意：如果采用分包上传则 file32 和 file64都必须指定文件")
	uploadCmd.Flags().StringVarP(&file64, "file64", "", "", "64位apk文件路径 注意：如果采用分包上传则 file32 和 file64都必须指定文件")

	// 如果分包，不能同时传单包和32位
	uploadCmd.MarkFlagsMutuallyExclusive("file", "file32")
	// 如果分包，不能同时传单包和64位
	uploadCmd.MarkFlagsMutuallyExclusive("file", "file64")

	// 如果分包，32位和64位必须同时传
	uploadCmd.MarkFlagsRequiredTogether("file32", "file64")

	// 更新日志
	uploadCmd.Flags().StringVarP(&releaseNots, "release-notes", "n", "性能优化、提升稳定性", "更新日志")

	// 是否需要禁用二次确认
	uploadCmd.Flags().BoolVar(&disableDoubleCheck, "disable-double-confirmation", false, "取消二次确认")

}

func runUpload(cmd *cobra.Command, args []string) {

	defer func() {
		// 清理一些需要关闭的publisher
		for _, p := range publishers {
			if post, ok := p.(shared.PostPublish); ok {
				if err := post.PostDo(); err != nil {
					fmt.Println(text.FgRed.Sprintf("清理资源出错. %s", err.Error()))
				}
			}
		}
	}()

	req := assemblePublishRequest()

	fmt.Println()
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendRow(table.Row{
		fmt.Sprintf("Name: %s\nVersion: %s\nApplicationID: %s\nReleaseNotes: %s\nUploadStores: %s",
			text.FgGreen.Sprint(req.AppName),
			text.FgGreen.Sprintf("%s+%d", req.VersionName, req.VersionCode),
			text.FgGreen.Sprint(req.PackageName),
			text.FgGreen.Sprint(req.UpdateDesc),
			text.FgYellow.Sprint(req.Stores),
		),
	})
	ns := []string{}
	if config.Notifiers.Lark != nil {
		ns = append(ns, "飞书")
	}
	if config.Notifiers.DingTalk != nil {
		ns = append(ns, "钉钉")
	}
	if config.Notifiers.WeCom != nil {
		ns = append(ns, "企业微信")
	}
	if config.Notifiers.WebHook != nil {
		ns = append(ns, "WebHook")
	}
	if len(ns) > 0 {
		t.AppendSeparator()
		t.AppendRow(table.Row{
			fmt.Sprintf("Notifiers: %s", text.FgCyan.Sprint(strings.Join(ns, ","))),
		})
	}

	t.Render()

	// 是否需要二次确认
	if !disableDoubleCheck {
		for {
			reader := bufio.NewReader(os.Stdin)
			fmt.Printf("\n确认以上信息开始上传？(%s)\n", text.FgCyan.Sprint("yes/no"))
			y, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println(err)
				os.Exit(4)
			}
			input := strings.Trim(y, "\n")

			if input == "no" {
				os.Exit(0)
			}
			if input == "yes" {
				break
			}
		}
	}

	// 初始化所有商店的 Publisher
	if err := initialPublishers(true); err != nil {
		fmt.Printf("%s\n", text.FgRed.Sprintf("初始化应用商店上传组件失败 err: %s", err.Error()))
		os.Exit(5)
	}

	// 开始上传
	fmt.Println()
	result := publish(req)

	// 通知
	if err := notify(req, result); err != nil {
		fmt.Printf("%s\n", text.FgRed.Sprintf("上传结果通知失败 err: %s", err.Error()))
		os.Exit(6)
	}

	// 记录节省时间
	// 商店数 * 5 分钟
	http.Post("https://central.rainbowbridge.top/api/apkgo/", "text/plain", strings.NewReader(strings.Join(stores, ",")))

	fmt.Println(text.FgYellow.Sprint("Finished 🚀🚀"))
}

func validateApkFile(f string) error {
	if _, err := apk.OpenFile(f); err != nil {
		return err
	}
	return nil
}
