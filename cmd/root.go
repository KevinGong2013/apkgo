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

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "apkgo",
	Short: fmt.Sprintf("中国安卓应用分发渠道更新工具。项目主页：%s", text.FgCyan.Sprint("https://apkgo.com.cn")),
}

var isDebugMode = true

func Execute(isRelease bool) {
	isDebugMode = !isRelease
	if isDebugMode {
		fmt.Println(text.FgHiYellow.Sprint("Debug mode will use mock publisher \n"))
	}
	err := rootCmd.Execute()

	if err != nil {
		os.Exit(1)
	}
}

var (
	developMode bool
	secretsFile string
)

func init() {

	rootCmd.PersistentFlags().StringVar(&secretsFile, "secrets_file", "", "指定各应用商店秘钥文件路径，默认为 $HOME/.apkgo/secrets.json")
	rootCmd.PersistentFlags().BoolVar(&developMode, "develop_mode", false, "开发者模式打开，会输出Trace级别的plugin日志")

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	if len(secretsFile) == 0 {
		home, err := homedir.Dir()
		if err != nil {
			panic(err)
		}
		secretsFile = filepath.Join(home, ".apkgo", "secrets.json")
	}

	cobra.OnInitialize(initConfig)
}

func initConfig() {
	fmt.Println(text.FgCyan.Sprint("store secrets config file path: ", secretsFile))
}
