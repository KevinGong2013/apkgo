package cmd

import (
	"os"
	"os/exec"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

type PluginConfig struct {
	ProtocolVersion  uint     `json:"version"`
	MagicCookieKey   string   `json:"magic_cookie_key"`
	MagicCookieValue string   `json:"magic_cookie_value"`
	Path             string   `json:"path"`
	Name             string   `json:"name"`
	Args             []string `json:"args,omitempty"`
}

type PluginPublisher struct {
	plugin    *plugin.Client
	publisher shared.Publisher
}

func NewPluginPublisher(pc *PluginConfig) (*PluginPublisher, error) {

	c := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  pc.ProtocolVersion,
			MagicCookieKey:   pc.MagicCookieKey,
			MagicCookieValue: pc.MagicCookieValue,
		},
		Cmd: exec.Command(pc.Path, pc.Args...),
		Plugins: map[string]plugin.Plugin{
			pc.Name: &shared.PublisherPlugin{},
		},
		Logger: hclog.New(&hclog.LoggerOptions{
			Output: os.Stdout,
			Level:  hclog.Trace,
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

	return &PluginPublisher{
		publisher: raw.(shared.Publisher),
		plugin:    c,
	}, nil
}

func (pp *PluginPublisher) Name() string {
	return pp.publisher.Name()
}

func (pp *PluginPublisher) Do(req shared.PublishRequest) error {
	return pp.publisher.Do(req)
}

func (pp *PluginPublisher) Close() error {
	pp.plugin.Kill()
	return nil
}
