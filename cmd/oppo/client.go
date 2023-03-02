package oppo

import (
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type Client struct {
	page *rod.Page
}

func NewClient(dir string) (*Client, error) {
	u, err := launcher.New().
		UserDataDir(dir).
		ProfileDir("apkgo").
		Headless(false).
		Set("disable-gpu").
		Set("disable-features", "OptimizationGuideModelDownloading,OptimizationHintsFetching,OptimizationTargetPrediction,OptimizationHints").
		Launch()
	if err != nil {
		return nil, err
	}

	b := rod.New().ControlURL(u)

	if err := b.Connect(); err != nil {
		return nil, err
	}

	page, err := b.Page(proto.TargetCreateTarget{
		URL: "https://open.oppomobile.com/new/ecological/app",
	})
	if err != nil {
		return nil, err
	}
	return &Client{
		page: page,
	}, nil
}

func (c *Client) Name() string {
	return "oppo应用商店"
}

func (c *Client) CheckAuth(reAuth bool) error {
	return c.check(reAuth)
}

func (c *Client) Do(req shared.PublishRequest) error {
	return c.do(req)
}

func (c *Client) PostDo() error {

	if err := c.page.Close(); err != nil {
		return err
	}

	return c.page.Browser().Close()
}
