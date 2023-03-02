package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/KevinGong2013/apkgo/cmd/fir"
	"github.com/KevinGong2013/apkgo/cmd/huawei"
	"github.com/KevinGong2013/apkgo/cmd/notifiers"
	"github.com/KevinGong2013/apkgo/cmd/oppo"
	"github.com/KevinGong2013/apkgo/cmd/pgyer"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/vivo"
	"github.com/KevinGong2013/apkgo/cmd/xiaomi"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/jedib0t/go-pretty/v6/text"
)

type Store struct {
	Name    string `json:"name"`
	Key     string `json:"key,omitempty"`
	Secret  string `json:"secret,omitempty"`
	disable bool
}

type PluginStore struct {
	Store
	ProtocolVersion  int      `json:"version"`
	MagicCookieKey   string   `json:"magic_cookie_key"`
	MagicCookieValue string   `json:"magic_cookie_value"`
	Path             string   `json:"path"`
	Author           string   `json:"author"`
	Args             []string `json:"args,omitempty"`
}

type StoreConfig struct {
	Stores struct {
		Curls    []*Store       `json:"curls"`
		Browsers []*Store       `json:"browsers"`
		Plugins  []*PluginStore `json:"plugins"`
	} `json:"stores"`
	Notifiers struct {
		Lark     *notifiers.LarkNotifier     `json:"lark"`
		Dingtalk *notifiers.DingTalkNotifier `json:"dingtalk"`
		Wecom    *notifiers.WeComNotifier    `json:"wecom"`
		Webhook  *notifiers.Webhook          `json:"webhook"`
	} `json:"notifiers"`
}

// TYPE 1
func NewCurlClient(name, key, secret string) (shared.Publisher, error) {
	switch name {
	case "huawei":
		return huawei.NewClient(key, secret)
	case "xiaomi":
		return xiaomi.NewClient(key, secret)
	case "vivo":
		return vivo.NewClient(key, secret)
	case "pgyer":
		return pgyer.NewClient(key), nil
	case "fir":
		return fir.NewClient(key), nil
	default:
		return &mockPublisher{
			name:   name,
			key:    key,
			secret: secret,
		}, nil
	}
}

func NewChromePublisher(store string) (shared.Publisher, error) {
	dir := filepath.Join(apkgoHome, SecretDirName, "chrome_user_data")
	os.MkdirAll(dir, 0666)
	switch store {
	case "oppo":
		return oppo.NewClient(dir)
	default:
		return nil, fmt.Errorf("unsupported store. [%s]", store)
	}
}

// TYPE3
func NewPluginPublisher(pc *PluginStore) (*PluginPublisher, error) {

	logger := hclog.New(&hclog.LoggerOptions{
		Output: os.Stdout,
		Level:  hclog.Error,
		Name:   "PluginPublisher",
	})
	if developMode {
		logger.SetLevel(hclog.Trace)
	}

	c := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  uint(pc.ProtocolVersion),
			MagicCookieKey:   pc.MagicCookieKey,
			MagicCookieValue: pc.MagicCookieValue,
		},
		Cmd: exec.Command(pc.Path, pc.Args...),
		Plugins: map[string]plugin.Plugin{
			pc.Name: &shared.PublisherPlugin{},
		},
		Logger: logger,
	})

	rpcClient, err := c.Client()
	if err != nil {
		return nil, err
	}

	raw, err := rpcClient.Dispense(pc.Name)
	if err != nil {
		return nil, err
	}

	return &PluginPublisher{
		publisher: raw.(shared.Publisher),
		plugin:    c,
	}, nil
}

// 解析Store配置文件
func ParseStoreSecretFile(identifiers []string) (*StoreConfig, error) {

	if len(identifiers) == 0 {
		identifiers = append(identifiers, "all")
	}

	storeCfgFile := storeCfgFilePath()

	bytes, err := os.ReadFile(storeCfgFile)
	if err != nil {
		return nil, err
	}

	var conf StoreConfig

	// 解析config文件
	if err = json.Unmarshal(bytes, &conf); err != nil {
		// fmt.Println(text.FgRed.Sprintf("Config文件解析失败 %s", err.Error()))
		return nil, err
	}

	if len(conf.Stores.Plugins) > 0 {
		// 从本地整体配置中匹配插件地址
		c, err := LoadConfig()
		if err != nil {
			return nil, err
		}

		for _, p := range conf.Stores.Plugins {
			for _, i := range c.Plugins {
				if p.Name == i.Name {
					p.Path = i.Path
					break
				}
			}
			if len(p.Path) == 0 {
				fmt.Println(text.FgYellow.Sprintf("插件%s.Path未配置", p.Name))
			}
		}
	}

	// 根据 identifiers  将不需要的商店过滤掉
	if len(identifiers) == 0 || (len(identifiers) == 1 && identifiers[0] == "all") {
		return &conf, nil
	}

	s := strings.Join(identifiers, " ")
	for _, c := range conf.Stores.Curls {
		c.disable = !strings.Contains(s, c.Name)
	}
	for _, b := range conf.Stores.Browsers {
		b.disable = !strings.Contains(s, b.Name)
	}
	for _, p := range conf.Stores.Plugins {
		p.disable = !strings.Contains(s, p.Name)
	}

	return &conf, nil
}

func InitPublishers(sc *StoreConfig, browserHeadless bool) (curls []shared.Publisher, browsers []shared.Publisher, plugins []shared.Publisher, err error) {

	var p shared.Publisher

	// type 1
	for _, c := range sc.Stores.Curls {
		if c.disable {
			continue
		}
		if p, err = NewCurlClient(c.Name, c.Key, c.Secret); err != nil {
			return
		}
		curls = append(curls, p)
	}

	// type 2
	if len(sc.Stores.Browsers) > 0 {
		for _, b := range sc.Stores.Browsers {
			if b.disable {
				continue
			}
			if p, err = NewChromePublisher(b.Name); err != nil {
				return
			}
			browsers = append(browsers, p)
		}
	}

	// type 3
	for _, pc := range sc.Stores.Plugins {
		if pc.disable {
			continue
		}
		if p, err = NewPluginPublisher(pc); err != nil {
			return
		}
		plugins = append(plugins, p)
	}

	return
}
