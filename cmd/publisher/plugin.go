package publisher

import (
	"os"
	"os/exec"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

type PluginPublisher struct {
	plugin    *plugin.Client
	publisher shared.Publisher
}

type Store struct {
	Name    string `json:"name"`
	Key     string `json:"key,omitempty"`
	Secret  string `json:"secret,omitempty"`
	Disable bool
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

func NewPluginPublisher(pc *PluginStore, developMode bool) (*PluginPublisher, error) {

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
