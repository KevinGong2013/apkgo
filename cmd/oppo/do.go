package oppo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/utils"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func (c *Client) do(req shared.PublishRequest) error {

	taskCtx := c.ctx

	js := `
async function postData(data = {}) {
  const response = await fetch('https://open.oppomobile.com/resource/list/index.json', {
    method: 'POST',
	body: data
  });
  return response.text();
}; 

let formData = new FormData();
formData.append('type', 0);
formData.append('limit', 100);
formData.append('offset', 0);
formData.append('app_name', '寓小二公寓版');

postData(formData)`
	var response string
	if err := chromedp.Run(taskCtx,
		chromedp.Evaluate(js, &response, func(ep *runtime.EvaluateParams) *runtime.EvaluateParams {
			return ep.WithAwaitPromise(true)
		}),
	); err != nil {
		log.Fatal(err)
	}

	var indexResponse IndexResponse

	if err := json.Unmarshal([]byte(response), &indexResponse); err != nil {
		log.Fatalf("unmarshal json failed %s", err.Error())
		return err
	}

	var app App

	for _, r := range indexResponse.Data.Apps {
		if r.PkgName == "com.yuxiaor" {
			app = r
			break
		}
	}
	if len(app.PkgName) == 0 {
		// 找不到app
		return errors.New("not found")
	}

	// 做一些版本处理
	if err := chromedp.Run(taskCtx,
		chromedp.Navigate(fmt.Sprintf("https://open.oppomobile.com/new/mcom#/home/management/app-admin#/resource/update/index?app_id=%s&is_gray=2", app.AppID)),
	); err != nil {
		log.Fatal(err)
	}

	chromedp.Run(taskCtx,
		utils.RunWithTimeOut(&taskCtx, 3, chromedp.Tasks{
			chromedp.Click(`button[class="btn yes"]`),
		}),
	)

	var iframes []*cdp.Node
	if err := chromedp.Run(taskCtx, chromedp.Nodes(`iframe[id="menu_service_main_iframe"]`, &iframes, chromedp.ByQuery)); err != nil {
		return err
	}
	if len(iframes) == 0 {
		log.Fatal("no iframe")
		return errors.New("no iframe")
	}

	mainIframe := iframes[0]

	ch := make(chan *network.EventResponseReceived)
	// 监听网络等待文件上传成功
	chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		go func() {
			if res, ok := ev.(*network.EventResponseReceived); ok {
				ch <- res
			}
		}()
	})

	// 上传文件
	if err := chromedp.Run(taskCtx,
		chromedp.SetUploadFiles(`input[type="file"]`, []string{"/Users/gix/Downloads/Yuxiaor_release_9.2.8_9572.apk"}, chromedp.ByQuery, chromedp.FromNode(mainIframe)),
	); err != nil {
		log.Fatal(err)
		return err
	}

	fmt.Println("upload button ready")

	// 网络
	for res := range ch {
		if strings.Contains(res.Response.URL, "resource/parse/verify-info.json") {
			var respBody []byte
			if err := chromedp.Run(taskCtx, chromedp.ActionFunc(func(cxt context.Context) error {
				var err error
				respBody, err = network.GetResponseBody(res.RequestID).Do(cxt)
				return err
			})); err != nil {
				log.Fatal(err)
			}
			var result struct {
				ErrNo int `json:"errno"`
			}
			if err := json.Unmarshal(respBody, &result); err != nil {
				log.Fatal(err)
				return err
			}
			if result.ErrNo == 0 {
				break
			}
		}
	}

	updateDesc := "提升稳定性、优化性能"

	if err := chromedp.Run(taskCtx,
		chromedp.SetValue(`textarea[name="update_desc"]`, updateDesc, chromedp.ByQuery, chromedp.FromNode(mainIframe)),
	); err != nil {
		log.Fatal(err)
		return err
	}

	if err := chromedp.Run(taskCtx,
		chromedp.Click(`//*[@id="auditphasedbuttonclick"]`),
	); err != nil {
		log.Fatal(err)
		return err
	}

	fmt.Println("publish")

	// 尝试发布
	return nil
}
