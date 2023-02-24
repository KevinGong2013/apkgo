package oppo

import (
	"context"
	"log"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/chromedp/chromedp"
)

type Client struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func NewClient(ctx context.Context) (*Client, error) {
	taskCtx, cancel := chromedp.NewContext(ctx, chromedp.WithLogf(log.Printf))

	return &Client{
		ctx:    taskCtx,
		cancel: cancel,
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
	c.cancel()
	return nil
}
