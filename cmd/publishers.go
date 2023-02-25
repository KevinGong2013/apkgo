package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/KevinGong2013/apkgo/cmd/fir"
	"github.com/KevinGong2013/apkgo/cmd/huawei"
	"github.com/KevinGong2013/apkgo/cmd/oppo"
	"github.com/KevinGong2013/apkgo/cmd/pgyer"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/vivo"
	"github.com/KevinGong2013/apkgo/cmd/xiaomi"
	"github.com/chromedp/chromedp"
)

var browserCtx *context.Context
var browserCancelFunc context.CancelFunc

func NewChromeContext(headless bool) (context.Context, context.CancelFunc) {

	if browserCtx != nil {
		return *browserCtx, browserCancelFunc
	}

	dir := filepath.Join(apkgoHome, "chrome-user-data")

	os.MkdirAll(dir, 0755)

	opts := append(chromedp.DefaultExecAllocatorOptions[0:2],
		chromedp.DefaultExecAllocatorOptions[3:]...,
	)
	if headless {
		opts = append(opts, chromedp.Headless)
	}

	opts = append(opts, chromedp.UserDataDir(dir))

	ctx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)

	browserCtx = &ctx
	browserCancelFunc = cancel

	return *browserCtx, browserCancelFunc
}

func NewChromePublisher(ctx context.Context, store string) (shared.Publisher, error) {
	switch store {
	case "oppo":
		return oppo.NewClient(ctx)
	default:
		return nil, fmt.Errorf("unsupported store. [%s]", store)
	}
}

func initialPublishers(headless bool) error {

	config, err := parseStoreSecretFile()
	if err != nil {
		return err
	}

	for _, k := range stores {
		v := config.Publishers[k]
		switch k {
		case "xiaomi":
			xm, err := xiaomi.NewClient(v["username"], v["private_key"])
			if err != nil {
				return err
			}
			publishers[k] = xm
		case "vivo":
			vv, err := vivo.NewClient(v["access_key"], v["access_secret"])
			if err != nil {
				return err
			}
			publishers[k] = vv
		case "huawei":
			hw, err := huawei.NewClient(v["client_id"], v["client_secret"])
			if err != nil {
				return err
			}
			publishers[k] = hw
		case "pgyer":
			publishers[k] = pgyer.NewClient(v["api_key"])
		case "fir":
			publishers[k] = fir.NewClient(v["api_token"])
		default:
			// 只配置了名称，这种情况一律任务需要 chromedp
			if len(v) == 0 {
				ctx, _ := NewChromeContext(headless)
				p, err := NewChromePublisher(ctx, k)
				if err != nil {
					return err
				}
				publishers[k] = p
			}
			// 看看是不是支持的plugin
			if v["magic_cookie_key"] != "" && v["magic_cookie_value"] != "" {

				version, err := strconv.Atoi(v["version"])
				if err != nil {
					return err
				}

				p, err := NewPluginPublisher(&PluginConfig{
					Name:             k,
					Path:             v["path"],
					ProtocolVersion:  uint(version),
					MagicCookieKey:   v["magic_cookie_key"],
					MagicCookieValue: v["magic_cookie_value"],
				})
				if err != nil {
					return nil
				}
				publishers[k] = p
			} else {
				//
				return fmt.Errorf("unsupported market. [%s]", k)
			}
		}
	}

	return nil
}
