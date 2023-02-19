package plugin

import (
	"os/exec"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/hashicorp/go-plugin"
)

type Client struct {
	rawPublisher shared.Publisher
}

type Config struct {
	ProtocolVersion  uint   `json:"version"`
	MagicCookieKey   string `json:"magic_cookie_key"`
	MagicCookieValue string `json:"magic_cookie_value"`
	Path             string `json:"path"`
	Name             string `json:"name"`
}

func NewClient(pc *Config) (*Client, error) {

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
	})

	rpcClient, err := c.Client()
	if err != nil {
		return nil, err
	}

	raw, err := rpcClient.Dispense(pc.Name)
	if err != nil {
		return nil, err
	}

	return &Client{
		rawPublisher: raw.(shared.Publisher),
	}, nil
}

func (c *Client) Name() string {
	return c.rawPublisher.Name()
}

func (c *Client) Do(req shared.PublishRequest) error {
	return c.rawPublisher.Do(req)
}
