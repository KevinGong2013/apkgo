package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

var logger = hclog.New(&hclog.LoggerOptions{
	Output: hclog.DefaultOutput,
	Level:  hclog.Trace,
	Name:   "apkgo_demo",
})

func main() {

	plugin.Serve(&plugin.ServeConfig{
		Plugins: map[string]plugin.Plugin{
			"apkgo_demo": &shared.PublisherPlugin{Impl: &DemoPlugin{}},
		},
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  23,
			MagicCookieKey:   "apkgo_demo_key",
			MagicCookieValue: "apkgo_demo_value",
		},
		Logger: logger,
	})
}

type DemoPlugin struct{}

func (p *DemoPlugin) Name() string {
	logger.Info("Plugin.Name()")
	return "apkgo_plugin_demo"
}

func (p *DemoPlugin) Do(req shared.PublishRequest) error {
	logger.Info("Plugin.Do() %s", req)

	r := rand.Intn(10)
	time.Sleep(time.Second * time.Duration(10+r))

	if r%2 == 0 {
		return fmt.Errorf("upload %s failed", req.AppName)
	}

	return nil
}
