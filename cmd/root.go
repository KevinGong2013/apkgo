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

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "apkgo",
	Short: fmt.Sprintf("中国安卓应用分发渠道更新工具。项目主页：%s", text.FgBlue.Sprint("https://apkgo.com.cn")),
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
}
