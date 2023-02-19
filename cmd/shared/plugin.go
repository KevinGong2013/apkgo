package shared

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

type Publisher interface {
	Do(req PublishRequest) error
	Name() string
}

// /////////////////////////
type PublisherRPC struct {
	client *rpc.Client
}

func (p *PublisherRPC) Name() string {
	var name string
	err := p.client.Call("Publisher.Name", new(interface{}), &name)
	if err != nil {
		panic(err)
	}
	return name
}

func (p *PublisherRPC) Do(req PublishRequest) error {
	return p.client.Call("Publisher.Do", req, nil)
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

func (s *PublisherRPCServer) Do(req *PublishRequest, resp interface{}) error {
	return s.Impl.Do(*req)
}

type PublisherPlugin struct {
	Impl Publisher
}

func (p *PublisherPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &PublisherRPCServer{Impl: p.Impl}, nil
}

func (PublisherPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return PublisherRPC{client: c}, nil
}
