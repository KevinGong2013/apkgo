package cmd

import (
	"os/exec"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

type PluginConfig struct {
	ProtocolVersion  uint   `json:"version"`
	MagicCookieKey   string `json:"magic_cookie_key"`
	MagicCookieValue string `json:"magic_cookie_value"`
	Path             string `json:"path"`
	Name             string `json:"name"`
}

func NewPluginPublisher(pc *PluginConfig) (shared.Publisher, error) {

	c := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  pc.ProtocolVersion,
			MagicCookieKey:   pc.MagicCookieKey,
			MagicCookieValue: pc.MagicCookieValue,
		},
		Cmd: exec.Command(pc.Path),
		Plugins: map[string]plugin.Plugin{
			pc.Name: &shared.PublisherPlugin{},
		},
		Logger: hclog.New(&hclog.LoggerOptions{
			Output: hclog.DefaultOutput,
			Level:  hclog.Error,
			Name:   "PluginPublisher",
		}),
	})

	rpcClient, err := c.Client()
	if err != nil {
		return nil, err
	}

	raw, err := rpcClient.Dispense(pc.Name)
	if err != nil {
		return nil, err
	}

	return raw.(shared.Publisher), nil
}
