package qh360

import (
	"errors"
	"fmt"
	"strings"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

type QH360Client string

var DefaultClient = QH360Client("qh360")

func (qc QH360Client) Identifier() string {
	return string(qc)
}

func (qc QH360Client) Name() string {
	return "360移动开放平台"
}

func (qc QH360Client) CheckAuth(browser *rod.Browser, reAuth bool) (*rod.Page, error) {
	page, err := browser.Page(proto.TargetCreateTarget{
		URL: "https://dev.360.cn/mod3/mobilenavs/index",
	})

	if err != nil {
		return nil, err
	}

	_, err = page.Race().Element("#content-main").MustHandle(func(e *rod.Element) {
		fmt.Println("qh360 login succeed.")
	}).Element(".quc-login-content").Handle(func(e *rod.Element) error {
		if !reAuth {
			return errors.New("鉴权失效")
		}
		fmt.Println("show login alert")
		if _, err := page.Eval("(msg) => { alert(msg) }", "登录完成以后会自动同步到apkgo"); err != nil {
			return err
		}
		return page.WaitElementsMoreThan("#content-main", 0)
	}).Do()

	return page, err
}

func (qc QH360Client) Do(page *rod.Page, req shared.PublishRequest) error {
	// 拿到appid
	js := `async function getAppList() {
	const response = await fetch('https://dev.360.cn/mod3/mobile/Newgetappinfopage?page=1&page_size=30&cate1=&condition=')
	return response.text()
}`

	obj, err := page.Evaluate(&rod.EvalOptions{
		ByValue:      true,
		AwaitPromise: true,
		JS:           js,
		UserGesture:  true,
	})
	if err != nil {
		return err
	}

	r := gson.NewFrom(obj.Value.Str())

	errno := r.Get("errno").Int()
	if errno != 0 {
		return fmt.Errorf("auth failed. %s", r.Raw())
	}

	var appid string
	for _, row := range r.Get("data").Get("list").Arr() {
		if row.Get("pname").Str() == req.PackageName {
			appid = row.Get("appid").Str()
			break
		}
	}
	if len(appid) == 0 {
		return fmt.Errorf("app not found. %s", req.PackageName)
	}

	return rod.Try(func() {
		page.MustNavigate(fmt.Sprintf("https://dev.360.cn/mod3/createmobile/baseinfo?id=%s", appid))

		wait := make(chan bool)
		go page.EachEvent(func(e *proto.NetworkResponseReceived) {
			url := e.Response.URL
			if strings.Contains(url, "mod/upload/apk/") || strings.Contains(url, "mod3/createmobile/submitBaseInfo") {
				m := proto.NetworkGetResponseBody{RequestID: e.RequestID}
				if r, err := m.Call(page); err == nil {
					body := gson.NewFrom(r.Body)
					if strings.Contains(url, "mod/upload/apk/") {
						wait <- body.Get("status").Int() == 0
					} else {
						wait <- body.Get("errno").Int() == 0
					}
				}
			}
		})()

		// 传文件
		go page.MustElement(`input[type="file"]`).MustSetFiles(req.ApkFile)
		<-wait

		// 更新文案
		page.MustElement("#desc_desc").MustSelectAllText().MustInput(req.UpdateDesc)

		// 同意协议
		has, btn, err := page.Has("#protocol")
		if err != nil {
			panic(err)
		}
		if has {
			// 同意360用户服务协议
			if btn.MustVisible() {
				btn.MustClick()
			}
		}

		go page.MustElement("#submitform").MustClick()
		<-wait
	})
}
