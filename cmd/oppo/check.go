package oppo

import (
	"log"

	"github.com/KevinGong2013/apkgo/cmd/utils"
	"github.com/chromedp/chromedp"
)

func (c *Client) check(reAuth bool) error {
	ctx := c.ctx

	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://open.oppomobile.com/new/ecological/app"),
	); err != nil {
		log.Fatal(err)
		return nil
	}

	if err := chromedp.Run(ctx, utils.RunWithTimeOut(&ctx, 10, chromedp.Tasks{
		chromedp.WaitVisible(".service-item-open"),
	})); err != nil {
		log.Fatal("需要登陆")
		return err
	}

	return nil

}
