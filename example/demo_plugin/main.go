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
	Level:  hclog.Error,
	Name:   "apkgo_demo_plugin",
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
	logger.Debug("Name()")
	return "apkgo_plugin_demo"
}

func (p *DemoPlugin) Do(req shared.PublishRequest) error {
	r := rand.Intn(3)
	time.Sleep(time.Second * time.Duration(2+r))

	logger.Debug("Do", r%2)

	if r%2 == 0 {
		return fmt.Errorf("upload %v failed", req)
	}

	return nil

}
