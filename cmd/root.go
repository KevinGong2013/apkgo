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
	"fmt"
	"os"
	"path/filepath"

	"github.com/KevinGong2013/apkgo/cmd/notifiers"
	"github.com/spf13/cobra"

	homedir "github.com/mitchellh/go-homedir"
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
	// Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		cfgFile = "/Users/gix/Documents/GitHub/apkgo/.apkgo.json"

		err := InitialPublishers([]string{"cams", "apkgo_demo"})
		if err != nil {
			fmt.Println(err)
			return
		}

		Do("1. 提升稳定性\n2.优化性能", "/Users/gix/Documents/aster/build/app/outputs/flutter-apk/app-release.apk")

	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

var cfgFile string
var apkFile string
var apk32File string
var apk64File string

// var updateDesc string

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.apkgo.yaml)")
	rootCmd.PersistentFlags().StringVar(&apkFile, "apk", "", "单包apk文件路径")
	rootCmd.PersistentFlags().StringVar(&apk32File, "apk32", "", "32位apk文件路径")
	rootCmd.PersistentFlags().StringVar(&apk64File, "apk64", "", "64位apk文件路径")
	rootCmd.Flag("e")
	// rootCmd.PersistentFlags().StringArray()

	rootCmd.AddCommand(versionCmd)
}

type Config struct {
	Publishers map[string]map[string]string `json:"publishers"`
	Notifiers  struct {
		Lark     *notifiers.LarkNotifier     `json:"lark,omitempty"`
		DingTalk *notifiers.DingTalkNotifier `json:"dingtalk,omitempty"`
		WeCom    *notifiers.WeComNotifier    `json:"wecom,omitempty"`
		WebHook  *struct {
			Url string `json:"url"`
		}
	} `json:"notifiers,omitempty"`
}

func initConfig() {
	if cfgFile == "" {
		home, err := homedir.Dir()
		if err != nil {
			panic(err)
		}
		cfgFile = filepath.Join(home, ".apkgo.json")
	}
}
