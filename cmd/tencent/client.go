package tencent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

type TencentClient string

var DefaultClient = TencentClient("tencent")

func (tc TencentClient) Identifier() string {
	return string(tc)
}

func (tc TencentClient) Name() string {
	return "腾讯应用宝"
}

func (tc TencentClient) CheckAuth(browser *rod.Browser, reAuth bool) (*rod.Page, error) {
	page, err := browser.Page(proto.TargetCreateTarget{
		URL: "https://app.open.qq.com/p/app/list",
	})

	if err != nil {
		return nil, err
	}

	_, err = page.Race().Element(".app-list-main").MustHandle(func(e *rod.Element) {
		fmt.Println("鉴权有效")
	}).Element(".manage").Handle(func(e *rod.Element) error {
		if !reAuth {
			return errors.New("鉴权失效")
		}
		fmt.Println("登录用户登陆...")
		if _, err := page.Eval("(msg) => { alert(msg) }", "登录完成以后会自动同步到apkgo"); err != nil {
			return err
		}
		return page.WaitElementsMoreThan(".app-list-main", 0)
	}).Do()

	return page, err
}

func (tc TencentClient) Do(page *rod.Page, req shared.PublishRequest) error {

	//
	appIdCh := make(chan string)

	wait := make(chan bool)

	go page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		fmt.Println(e.Response.URL)
		if strings.Contains(e.Response.URL, "v3/get_app_list") ||
			strings.HasPrefix(e.Response.URL, "https://p.open.qq.com/open_file/v1/init_multi_upload") ||
			strings.HasPrefix(e.Response.URL, "https://app.open.qq.com/api/xy/runtime/env/prod/manage/datasource/collection/request/open/distribution_update_edit_v2/putOnAndUpdate/custom_commit") {
			m := proto.NetworkGetResponseBody{RequestID: e.RequestID}
			r, err := m.Call(page)
			if err != nil {
				fmt.Printf("fetch response %s failed \n", e.Response.URL)
			} else {
				body := gson.NewFrom(r.Body)

				if body.Get("ret").Int() == 0 {
					if strings.Contains(e.Response.URL, "v3/get_app_list") {
						for _, app := range body.Get("data").Get("apps").Arr() {
							if app.Get("package_name").Str() == req.PackageName {
								appIdCh <- app.Get("app_id").Str()
							}
						}
					} else {
						// 上传文件成功
						// 或者提交审核成功
						wait <- true
					}
				}

			}
		} else if strings.Contains(e.Response.URL, "distribution_update_edit_v2/putOnAndUpdate/request") {
			wait <- true
		}
		return false
	})()

	go page.MustReload()

	appId := <-appIdCh

	go page.MustNavigate(fmt.Sprintf("https://app.open.qq.com/p/basic/distribution/update/edit?appId=%s", appId))

	<-wait // load draft

	return rod.Try(func() {

		page.MustElementR("label", "版本特性说明").MustParent().MustParent().MustElement("textarea").MustSelectAllText().MustInput(req.UpdateDesc)

		page.MustElementR("label", "32位安装包").MustParent().MustParent().MustElement("input").SetFiles([]string{req.ApkFile})

		<-wait // uploaded
		if len(req.SecondApkFile) > 0 {
			fmt.Print(page.MustElementR("label", "64位安装包").MustParent())
			page.MustElement("#w-sys-page > div > div:nth-child(6) > form > div:nth-child(4) > div.ant-col.ant-col-4.ant-form-item-label.ant-form-item-label-left > label").
				MustParent().MustParent().MustElement("input").SetFiles([]string{req.SecondApkFile})
			<-wait // upload64
		}

		page.MustElementR("span", "提交审核").MustParent().MustClick()

		<-wait
	})
}
