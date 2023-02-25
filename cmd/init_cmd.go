package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/KevinGong2013/apkgo/cmd/storage"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var initCommand = &cobra.Command{
	Use:   "init",
	Short: "初始化apkgo",
	Run:   runInit,
}

func init() {

	rootCmd.AddCommand(initCommand)
}

type Config struct {
	Storage storage.Config `yaml:"storage"`
	Plugins []struct {
		Name   string `yaml:"name"`
		Path   string `yaml:"path"`
		Author string `yaml:"author"`
		Repo   string `yaml:"repo"`
	} `yaml:"plugins,omitempty"`
}

func LoadConfig() (*Config, error) {
	// Read the YAML file
	data, err := os.ReadFile(filepath.Join(apkgoHome, "config.yaml"))
	if err != nil {
		return nil, err
	}

	// Unmarshal the YAML data into a Config struct
	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func runInit(cmd *cobra.Command, args []string) {

	c, _ := LoadConfig()
	if c == nil {
		c = &Config{}
	}
	if len(c.Storage.Location) > 0 {
		clean := false
		prompt := &survey.Confirm{
			Message: "配置文件已存在，是否重新初始化？",
		}
		handleExit(survey.AskOne(prompt, &clean))

		if clean {
			os.Remove(filepath.Join(apkgoHome, ConfigFileName))
			os.RemoveAll(filepath.Join(apkgoHome, SecretDirName))
		}
	}

	for {
		// 初始化仓库
		var location string
		prompt := &survey.Select{
			Message: "请选择认证信息的存储方式",
			Options: []string{"git", "local"},
			Default: c.Storage.Location,
			Description: func(value string, index int) string {
				switch value {
				case "git":
					return fmt.Sprintf("适合多人协作、集成CI/CD %s", text.FgGreen.Sprint("[推荐]"))
				case "local":
					return "单机使用，简单易操作"
				default:
					return ""
				}
			},
		}
		handleExit(survey.AskOne(prompt, &location))
		c.Storage.Location = location

		//
		if location == "local" {
			// 创建目录
			if err := os.MkdirAll(filepath.Join(apkgoHome, SecretDirName), 0755); err != nil {
				fmt.Println(text.FgRed.Sprint("创建认证信息存储目录失败请重新配置Storage ", err.Error()))
			} else {
				break
			}
		} else {
			c.Storage.Location = "git"
			gitInitial(c)

			s, err := storage.New(c.Storage)
			if err != nil {
				fmt.Println(text.FgRed.Sprintf("未知location %s", err.Error()))
			} else {
				if err = s.Mkdir(filepath.Join(apkgoHome, SecretDirName)); err != nil {
					fmt.Println(text.FgRed.Sprintf("Storage 配置失败. %s", err.Error()))
				} else {
					break
				}
			}
		}
	}

	err := write(c)
	if err != nil {
		fmt.Println(text.FgRed.Sprintf("写入配置文件失败. %s", err.Error()))
		os.Exit(1)
	}

	fmt.Println(text.FgGreen.Sprint("apkgo 初始化完成🚀🚀"))
}

// "http://git.yuxiaor.com/yuxiaor-mobile/apkgo-conf.git",
// "git@git.yuxiaor.com:yuxiaor-mobile/apkgo-conf.git",

func gitInitial(c *Config) {
	gitURL := ""
	prompt := &survey.Input{
		Message: text.FgYellow.Sprintf("请新创建一个%s的git仓库，用来存储各应用商店的认证信息\nURL of the Git Repo: ", text.FgRed.Sprint("私有")),
		Default: c.Storage.URL,
	}
	handleExit(survey.AskOne(prompt, &gitURL, survey.WithValidator(func(input interface{}) error {
		if isValidGitRepoURL(input) {
			return nil
		} else {
			return errors.New("请输入合法的url,以git或者https开头")
		}
	}), survey.WithValidator(survey.Required)))
	c.Storage.URL = gitURL

	authMethod := ""
	if strings.HasPrefix(gitURL, "ssh") {
		authMethod = "ssh"
	} else {
		promptS := &survey.Select{
			Message: "请选择Git仓库认证方式",
			Options: []string{"basic", "ssh"},
			Default: "ssh",
			Description: func(value string, index int) string {
				if value == "ssh" {
					return "通过私钥认证"
				} else {
					return "通过 Username+Password 或者 Username+AccessToken 认证"
				}
			},
		}
		handleExit(survey.AskOne(promptS, &authMethod))
	}

	if authMethod == "ssh" {
		// 输入文件
		privateKey := ""
		prompt := &survey.Input{
			Message: "请输入私钥文件路径。注意不要使用.pub结尾的公钥\n",
			Default: c.Storage.Key,
			Suggest: func(toComplete string) []string {
				files, _ := filepath.Glob(toComplete + "*")
				return files
			},
		}
		handleExit(survey.AskOne(prompt, &privateKey, survey.WithValidator(survey.Required)))
		c.Storage.Key = privateKey

		password := ""
		promptP := &survey.Password{
			Message: "请输入私钥密码，若无密码可直接按Enter",
		}
		handleExit(survey.AskOne(promptP, &password))
		c.Storage.Password = password
	} else {
		username := ""
		prompt := &survey.Input{
			Message: "请输入git仓库认证用户名",
			Default: c.Storage.Username,
		}
		handleExit(survey.AskOne(prompt, &username, survey.WithValidator(survey.Required)))
		c.Storage.Username = username

		password := ""
		promptP := &survey.Password{
			Message: "请输入git仓库认证密码或AccessToken",
		}
		handleExit(survey.AskOne(promptP, &password, survey.WithValidator(survey.Required)))
		c.Storage.Password = password
	}
}

func handleExit(err error) {
	if err != nil && err == terminal.InterruptErr {
		fmt.Println(text.FgYellow.Sprint("取消"))
		os.Exit(0)
	}
}

func isValidGitRepoURL(url interface{}) bool {
	// Convert the interface{} to a string
	urlStr, ok := url.(string)
	if !ok {
		return false
	}

	// Regular expression for Git URL
	gitURLRegex := regexp.MustCompile(`^(git@|http:|https:\/\/)([\w\.@\:\/\-~]+)(\.git)?$`)

	// Match the URL against the regex
	return gitURLRegex.MatchString(urlStr)
}

func write(c *Config) error {
	bytes, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(apkgoHome, "config.yaml"), bytes, 0755)
}
