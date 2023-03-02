package cmd

import (
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/hashicorp/go-plugin"
)

type PluginConfig struct {
	ProtocolVersion  int      `json:"version"`
	MagicCookieKey   string   `json:"magic_cookie_key"`
	MagicCookieValue string   `json:"magic_cookie_value"`
	Path             string   `json:"path"`
	Name             string   `json:"name"`
	Author           string   `json:"author"`
	Args             []string `json:"args,omitempty"`
}

type PluginPublisher struct {
	plugin    *plugin.Client
	publisher shared.Publisher
}

func (pp *PluginPublisher) Name() string {
	return pp.publisher.Name()
}

func (pp *PluginPublisher) Do(req shared.PublishRequest) error {
	return pp.publisher.Do(req)
}

func (pp *PluginPublisher) PostDo() error {
	pp.plugin.Kill()
	return nil
}
