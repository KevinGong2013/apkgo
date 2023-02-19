package shared

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

type PublishRequest struct {
	AppName     string
	PackageName string
	VersionCode int32
	VersionName string

	ApkFile       string
	SecondApkFile string
	UpdateDesc    string

	// synchroType 更新类型：0=新增，1=更新包，2=内容更新
	SynchroType int
}

type Publisher interface {
	Do(req PublishRequest) error
	Name() string
}

// /////////////////////////
type PublisherRPC struct {
	client *rpc.Client
}

func (p *PublisherRPC) Name() string {
	var resp string
	err := p.client.Call("Plugin.Name", new(interface{}), &resp)
	if err != nil {
		panic(err)
	}
	return resp
}

func (p *PublisherRPC) Do(req PublishRequest) error {
	return p.client.Call("Plugin.Do", req, nil)
}

// /////////////////////////////
// Here is the RPC server that PublisherRPCServer talks to, conforming to
// the requirements of net/rpc
type PublisherRPCServer struct {
	Impl Publisher
}

func (s *PublisherRPCServer) Name(args interface{}, resp *string) error {
	*resp = s.Impl.Name()
	return nil
}

func (s *PublisherRPCServer) Do(req PublishRequest, resp interface{}) error {
	return s.Impl.Do(req)
}

type PublisherPlugin struct {
	Impl Publisher
}

func (p *PublisherPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &PublisherRPCServer{Impl: p.Impl}, nil
}

func (PublisherPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &PublisherRPC{client: c}, nil
}
