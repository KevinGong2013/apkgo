package oppo

import (
	"fmt"
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
		if !reAuth {
			return err
		}

		// 等待用户登陆
		fmt.Println("请在浏览器完成登陆, 然后点击右上角的管理中心。鉴权成功后会自动关闭页面")
		return chromedp.Run(c.ctx,
			chromedp.Click(".cookieconsent_button__2skxhz", chromedp.AtLeast(0)),
			chromedp.WaitVisible(".service-item-open"),
		)
	}

	return nil

}
