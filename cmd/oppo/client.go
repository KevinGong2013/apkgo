package oppo

import (
	"errors"
	"fmt"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type OppoClient string

var DefaultClient = OppoClient("oppo")

func (oc OppoClient) Identifier() string {
	return string(oc)
}

func (oc OppoClient) Name() string {
	return "oppo应用商店"
}

func (oc OppoClient) CheckAuth(browser *rod.Browser, reAuth bool) (*rod.Page, error) {
	page, err := browser.Page(proto.TargetCreateTarget{
		URL: "https://open.oppomobile.com/new/ecological/app",
	})
	if err != nil {
		return nil, err
	}
	// oppo 环境检测非常慢
	time.Sleep(time.Second * 15)
	_, err = page.Race().ElementR("h1", "Sign in").Handle(func(e *rod.Element) error {
		if !reAuth {
			return errors.New("登陆态失效")
		}
		fmt.Println("show login alert")
		if _, err := page.Eval("(msg) => { alert(msg) }", "登录完成以后会自动同步到apkgo"); err != nil {
			return err
		}
		return page.WaitElementsMoreThan(".service-item-open", 0)
	}).Element(".service-item-open").MustHandle(func(e *rod.Element) {
		fmt.Println("oppo login succeed")
	}).Do()

	return page, err
}

func (oc OppoClient) Do(page *rod.Page, req shared.PublishRequest) error {
	return do(page, req)
}
