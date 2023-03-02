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

	initCommand.Flags().Bool("local", false, "单机使用，不初始化团队合作配置")
	initCommand.Flags().String("git", "", "存储认证信息的repo 例如: https://github.com/KevinGong2013/apkgo-conf-repo.git")
	initCommand.MarkFlagsMutuallyExclusive("local", "git")

	initCommand.Flags().String("username", "", "git仓库登陆用户名")
	initCommand.Flags().String("private-key", "", "git仓库使用的私钥路径")
	initCommand.Flags().String("password", "", "git登陆密码或者私钥的密码")

	initCommand.MarkFlagsRequiredTogether("username", "password")
	initCommand.MarkFlagsMutuallyExclusive("username", "private-key")

	rootCmd.AddCommand(initCommand)
}

type Config struct {
	Storage storage.Config `yaml:"storage"`
	Plugins []struct {
		Name string `yaml:"name"`
		Path string `yaml:"path"`
	} `yaml:"plugins,omitempty"`
}

func LoadConfig() (*Config, error) {
	// Read the YAML file
	data, err := os.ReadFile(configFilePath())
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

	sc := c.Storage
	// 判断有没有参数， 如果有参数就直接想办法重写掉
	if local, err := cmd.Flags().GetBool("local"); err == nil && local {
		sc = storage.Config{
			Location: "local",
		}
	} else {
		//
		git, _ := cmd.Flags().GetString("git")
		if len(git) > 0 {
			username, _ := cmd.Flags().GetString("git-username")
			privateKey, _ := cmd.Flags().GetString("git-private-key")
			password, _ := cmd.Flags().GetString("git-password")

			sc = storage.Config{
				Location: "git",
				URL:      git,
				Username: username,
				Password: password,
				Key:      privateKey,
			}
		} else {
			if len(sc.Location) > 0 {
				clean := false
				prompt := &survey.Confirm{
					Message: "配置文件已存在，是否重新初始化？",
				}
				handleExit(survey.AskOne(prompt, &clean))

				if clean {
					os.Remove(filepath.Join(apkgoHome, ConfigFileName))
					os.RemoveAll(filepath.Join(apkgoHome, SecretDirName))
					sc = storageInitial(sc)
				}
			} else {
				sc = storageInitial(sc)
			}
		}
	}

	s, err := storage.New(sc, filepath.Join(apkgoHome, SecretDirName))

	if err != nil {
		fatalErr(err.Error())
	} else {
		if err = s.EnsureDir(); err != nil {
			fatalErr(err.Error())

		}
	}
	c.Storage = sc

	if err := writeConfigToFile(c); err != nil {
		fatalErr(text.FgRed.Sprintf("写入配置文件失败. %s", err.Error()))
	}

	p := storeCfgFilePath()

	// 判断如果没有默认配置文件就写一个
	if _, err := os.Stat(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 写如一个默认的配置文件
			defaultCfg := `{
	"stores": {
		"huawei": {
			"client_id": "[替换为你的值]",
			"client_secret": "[替换为你的值]"
		},
		"vivo": {
			"access_key": "[替换为你的值]",
			"access_secret": "[替换为你的值]"
		},
		"xiaomi": {
			"username": "[替换为你的值]",
			"private_key": "[替换为你的值]"
		},
		"pgyer": {
			"api_key": "[替换为你的值]"
		},
		"fir": {
			"api_token": "[替换为你的值]"
		},
		"oppo": {
			"enable": "false",
			"description": "需要通过浏览器登陆，完成以上信息配置后会自动开始添加"
		},
		"qh360": {
			"enable": "false",
			"description": "[TODO]需要通过浏览器登陆，完成以上信息配置后会自动开始添加"
		},
		"baidu": {
			"enable": "false",
			"description": "[TODO]需要通过浏览器登陆，完成以上信息配置后会自动开始添加"
		},
		"tencent":{
			"enable": "false",
			"description": "需要通过浏览器登陆，完成以上信息配置后会自动开始添加"
		}
	},
	"notifiers": {
		"lark": {
			"key": "[替换为你的值]",
			"secret_token": "[替换为你的值]"
		},
		"dingtalk": {
			"access_token": "[替换为你的值]",
			"secret_token": "[替换为你的值]"
		},
		"wecom": {
			"key": "[替换为你的值]"
		},
		"webhook": {
			"url": [
				"[替换为你的值]"
			]
		}
	}
}`
			os.WriteFile(p, []byte(defaultCfg), 0755)
		}
	}

	// 添加gitignore文件
	gitignore := `chrome_user_data/SingletonCookie
chrome_user_data/SingletonLock
chrome_user_data/SingletonSocket
.DS_Store
`
	if err = os.WriteFile(filepath.Join(apkgoHome, SecretDirName, ".gitignore"), []byte(gitignore), 0666); err != nil {
		fatalErr(err.Error())
	}

	// 主要是同步gitignore
	s.Sync()

	fmt.Printf("apkgo 初始化完成\n")

	// 画一个表格
}

func storageInitial(existConfig storage.Config) storage.Config {

	c := existConfig

	for {
		// 初始化仓库
		var location string
		prompt := &survey.Select{
			Message: "请选择认证信息的存储方式",
			Options: []string{"git", "local"},
			Default: existConfig.Location,
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
		c.Location = location

		//
		if location == "local" {
			// 创建目录
			if err := os.MkdirAll(filepath.Join(apkgoHome, SecretDirName), 0755); err != nil {
				fmt.Println(text.FgRed.Sprint("创建认证信息存储目录失败请重新配置Storage ", err.Error()))
			} else {
				break
			}
		} else {
			c.Location = "git"
			gitInitial(&c)
			break
		}
	}

	return c
}

func gitInitial(c *storage.Config) {
	gitURL := ""
	prompt := &survey.Input{
		Message: text.FgYellow.Sprintf("请新创建一个%s的git仓库，用来存储各应用商店的认证信息\nURL of the Git Repo: ", text.FgRed.Sprint("私有")),
		Default: c.URL,
	}
	handleExit(survey.AskOne(prompt, &gitURL, survey.WithValidator(func(input interface{}) error {
		if isValidGitRepoURL(input) {
			return nil
		} else {
			return errors.New("请输入合法的url,以git或者https开头")
		}
	}), survey.WithValidator(survey.Required)))
	c.URL = gitURL

	authMethod := ""
	if strings.HasPrefix(gitURL, "http") {
		authMethod = "basic"
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
			Default: c.Key,
			Suggest: func(toComplete string) []string {
				files, _ := filepath.Glob(toComplete + "*")
				return files
			},
		}
		handleExit(survey.AskOne(prompt, &privateKey, survey.WithValidator(survey.Required)))
		c.Key = privateKey

		password := ""
		promptP := &survey.Password{
			Message: "请输入私钥密码，若无密码可直接按Enter",
		}
		handleExit(survey.AskOne(promptP, &password))
		c.Password = password
	} else {
		username := ""
		prompt := &survey.Input{
			Message: "请输入git仓库认证用户名",
			Default: c.Username,
		}
		handleExit(survey.AskOne(prompt, &username, survey.WithValidator(survey.Required)))
		c.Username = username

		password := ""
		promptP := &survey.Password{
			Message: "请输入git仓库认证密码或AccessToken",
		}
		handleExit(survey.AskOne(promptP, &password, survey.WithValidator(survey.Required)))
		c.Password = password
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

func writeConfigToFile(c *Config) error {
	bytes, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(configFilePath(), bytes, 0666)
}
