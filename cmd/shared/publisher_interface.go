package shared

import (
	"fmt"
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

type PublishRequest struct {
	AppName       string `json:"appName"`
	PackageName   string `json:"packageName"`
	VersionCode   int32  `json:"versionCode"`
	VersionName   string `json:"versionName"`
	ApkFile       string `json:"apkFile"`
	SecondApkFile string `json:"secondApkFile"`
	UpdateDesc    string `json:"updateDesc"`
	// synchroType 更新类型：0=新增，1=更新包，2=内容更新
	SynchroType int `json:"synchroType"`
	// 要上传的所有商店
	Stores string `json:"stores"`
}

func (r PublishRequest) Version() string {
	return fmt.Sprintf("%s+%d", r.VersionName, r.VersionCode)
}

type Publisher interface {
	Do(req PublishRequest) error
	Name() string
}

type Checker interface {
	CheckAuth(reAuth bool) error
}

type PrePublish interface {
	PreDo(req PublishRequest) error
}

type PostPublish interface {
	PostDo() error
}

// /////////////////////////

type PublisherPlugin struct {
	Impl Publisher
}

type PublisherRPC struct {
	client *rpc.Client
}

func (p *PublisherRPC) Name() string {
	var resp string
	// ignore error
	err := p.client.Call("Plugin.Name", new(interface{}), &resp)
	if err != nil {
		fmt.Println(err)
		return "unknown_plugin"
	}
	return resp
}

func (p *PublisherRPC) Do(req PublishRequest) error {
	var reply string
	err := p.client.Call("Plugin.Do", req, &reply)

	return err
}

func (PublisherPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &PublisherRPC{client: c}, nil
}

// /////////////////////////////
// 插件内通过这个server来提供服务
type PublisherRPCServer struct {
	Impl Publisher
}

func (s *PublisherRPCServer) Name(args interface{}, resp *string) error {
	*resp = s.Impl.Name()
	return nil
}

func (s *PublisherRPCServer) Do(req PublishRequest, resp *string) error {
	err := s.Impl.Do(req)
	if err == nil {
		*resp = "NoError"
	}
	return err
}

func (p *PublisherPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &PublisherRPCServer{Impl: p.Impl}, nil
}
