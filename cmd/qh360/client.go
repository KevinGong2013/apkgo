package qh360

import (
	"errors"
	"fmt"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/utils"
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
		fmt.Println("鉴权有效")
	}).Element(".quc-login-content").Handle(func(e *rod.Element) error {
		if !reAuth {
			return errors.New("鉴权失效")
		}
		fmt.Println("登录用户登陆...")
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

		fmt.Println("等待上传完成。。。")
		if err := utils.WaitRequest(page, "mod/upload/apk/", func() {
			// 传文件
			page.MustElement(`input[type="file"]`).MustSetFiles(req.ApkFile)
		}, func(body gson.JSON) (stop bool, err error) {
			status := body.Get("status").Int()
			if status == 0 {
				return true, nil
			}
			return true, fmt.Errorf("err: %s", body.String())
		}); err != nil {
			panic(err)
		}

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

		if err := utils.WaitRequest(page, "mod3/createmobile/submitBaseInfo", func() {
			page.MustElement("#submitform").MustClick()
		}, func(body gson.JSON) (stop bool, err error) {
			errno := body.Get("errno").Int()
			if errno == 0 {
				return true, nil
			}
			return true, fmt.Errorf("err %s", body.String())
		}); err != nil {
			panic(err)
		}
	})
}
