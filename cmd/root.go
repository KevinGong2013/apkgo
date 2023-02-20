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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/KevinGong2013/apkgo/cmd/notifiers"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of apkgo",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("v0.9.0")
	},
}

var rootCmd = &cobra.Command{
	Use:   "apkgo",
	Short: "一键上传apk到 华为、小米、vivo、蒲公英、fir.im等",
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

var cfgFilePath string

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.AddCommand(versionCmd)
	rootCmd.PersistentFlags().StringVarP(&cfgFilePath, "config", "c", "", "config file (default is $HOME/.apkgo.json)")
}

var config Config

type Config struct {
	Publishers map[string]map[string]string `json:"stores"`
	Notifiers  struct {
		Lark     *notifiers.LarkNotifier     `json:"lark,omitempty"`
		DingTalk *notifiers.DingTalkNotifier `json:"dingtalk,omitempty"`
		WeCom    *notifiers.WeComNotifier    `json:"wecom,omitempty"`
		WebHook  *notifiers.Webhook          `json:"webhook,omitempty"`
	} `json:"notifiers,omitempty"`
}

func initConfig() {

	if cfgFilePath == "" {
		home, err := homedir.Dir()
		if err != nil {
			panic(err)
		}
		cfgFilePath = filepath.Join(home, ".apkgo.json")
	}

	fmt.Println(text.FgMagenta.Sprintf("config file -> %s", cfgFilePath))

	// 读取config文件
	cfgFileBytes, err := os.ReadFile(cfgFilePath)
	if err != nil {
		fmt.Println(text.FgRed.Sprintf("config文件读取失败 err: %s", err.Error()))
		os.Exit(1)
		return
	}

	// 解析config文件
	if err = json.Unmarshal(cfgFileBytes, &config); err != nil {
		fmt.Println(text.FgRed.Sprintf("config文件解析失败 err: %s", err.Error()))
		os.Exit(2)
		return
	}

	// 判断配置是否正确
	if len(config.Publishers) == 0 {
		fmt.Println(text.FgYellow.Sprint("没有可用store"))
		os.Exit(3)
	}

}
