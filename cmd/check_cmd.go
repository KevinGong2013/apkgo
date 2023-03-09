package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/storage"
	"github.com/KevinGong2013/apkgo/cmd/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

var checkCommand = &cobra.Command{
	Use:   "check",
	Short: "预检查各个平台的授权信息",
	Run:   runCheck,
}

func init() {

	rootCmd.AddCommand(checkCommand)

	checkCommand.Flags().Bool("refresh-cookie", true, "如果认证失败是否打开浏览器重新登陆以刷新cookie")
}

func runCheck(cmd *cobra.Command, args []string) {

	// 1. 读取配置文件
	c, _ := LoadConfig()
	if c == nil {
		fatalErr("请先完成初始化配置")
	}

	// 初始化存储
	s, err := storage.New(c.Storage, filepath.Join(apkgoHome, SecretDirName))
	if err != nil {
		fatalErr("storage配置不正确")
	}

	// 更新本地的认证信息到最新
	if err := s.UpToDate(); err != nil {
		fatalErr(err.Error())
	}

	// 解析config文件
	sc, err := ParseStoreSecretFile(args)
	if err != nil {
		fatalErr(err.Error())
	}

	// 检查没有插件，如果有插件检查一下插件有没有成功配置
	refreshCookie, err := cmd.Flags().GetBool("refresh-cookie")
	if err != nil {
		refreshCookie = true
	}
	if utils.IsRunningInDockerContainer() {
		refreshCookie = false
	}

	fmt.Println("Initial publishers ...")

	curls, browsers, plugins, err := InitPublishers(sc)

	if err != nil {
		fatalErr(err.Error())
	}

	fmt.Printf("curls(%d), browsers(%d), plugins(%d)\n", len(curls), len(browsers), len(plugins))

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)

	t.AppendHeader(table.Row{"name", "category", "status"})

	var rows []table.Row
	for _, plugin := range plugins {
		rows = append(rows, table.Row{plugin.Name(), "plugin", text.FgGreen.Sprint("正常")})
	}
	if len(rows) > 0 {
		t.AppendRows(rows, table.RowConfig{
			AutoMerge: false,
		})
		t.AppendSeparator()
		rows = nil
	}

	for _, curl := range curls {
		rows = append(rows, table.Row{curl.Name(), "api", text.FgGreen.Sprint("正常")})
	}

	if len(rows) > 0 {
		t.AppendRows(rows, table.RowConfig{
			AutoMerge: false,
		})
		t.AppendSeparator()
		rows = nil
	}

	// 测试用
	for _, p := range browsers {
		if c, ok := p.(shared.Checker); ok {
			status := text.FgGreen.Sprint("正常")
			if err := c.CheckAuth(refreshCookie); err != nil {
				if refreshCookie {
					status = text.FgRed.Sprintf("重新登陆失败: %s", err.Error())
				} else {
					status = text.FgYellow.Sprint("需要重新登陆")
				}
			}
			if post, ok := p.(shared.PublishCleaner); ok {
				if err := post.Clean(); err != nil {
					fmt.Println(text.FgRed.Sprintf("清理资源出错. %s", err.Error()))
				}
			}

			rows = append(rows, table.Row{p.Name(), "browser", status})
		}
	}
	t.AppendRows(rows)

	// 同步认证信息
	fmt.Println(text.FgYellow.Sprint("等待浏览器保存数据 2s"))
	time.Sleep(time.Second * 2)
	if err := s.Sync(); err != nil {
		fatalErr(err.Error())
	}

	t.Render()
}

func fatalErr(message string) {
	fmt.Println(text.FgRed.Sprintf("%s\n帮助文档: https://apkgo.com.cn", message))
	os.Exit(1)
}
