package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/KevinGong2013/apkgo/cmd/notifiers"
	"github.com/KevinGong2013/apkgo/cmd/publisher"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/jedib0t/go-pretty/v6/text"
)

type StoreConfig struct {
	Stores struct {
		Curls    []*publisher.Store       `json:"curls"`
		Browsers []*publisher.Store       `json:"browsers"`
		Plugins  []*publisher.PluginStore `json:"plugins"`
	} `json:"stores"`
	Notifiers struct {
		Lark     *notifiers.LarkNotifier     `json:"lark"`
		Dingtalk *notifiers.DingTalkNotifier `json:"dingtalk"`
		Wecom    *notifiers.WeComNotifier    `json:"wecom"`
		Webhook  *notifiers.Webhook          `json:"webhook"`
	} `json:"notifiers"`
}

// TYPE 1

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
		c.Disable = !strings.Contains(s, c.Name)
	}
	for _, b := range conf.Stores.Browsers {
		b.Disable = !strings.Contains(s, b.Name)
	}
	for _, p := range conf.Stores.Plugins {
		p.Disable = !strings.Contains(s, p.Name)
	}

	return &conf, nil
}

func InitPublishers(sc *StoreConfig, browserHeadless bool) (curls []shared.Publisher, browsers []shared.Publisher, plugins []shared.Publisher, err error) {

	var p shared.Publisher

	// type 1
	for _, c := range sc.Stores.Curls {
		if c.Disable {
			continue
		}
		if p, err = publisher.NewCurlClient(c.Name, c.Key, c.Secret); err != nil {
			return
		}
		curls = append(curls, p)
	}

	// type 2
	if len(sc.Stores.Browsers) > 0 {
		for _, b := range sc.Stores.Browsers {
			if b.Disable {
				continue
			}
			p, err = publisher.NewBrowserPublisher(b.Name, browserUserDataDir(), browserHeadless)
			if err != nil {
				return
			}
			browsers = append(browsers, p)
		}
	}

	// type 3
	for _, pc := range sc.Stores.Plugins {
		if pc.Disable {
			continue
		}
		if p, err = publisher.NewPluginPublisher(pc, developMode); err != nil {
			return
		}
		plugins = append(plugins, p)
	}

	return
}
