package oppo

import (
	"fmt"
	"strings"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

func do(page *rod.Page, req shared.PublishRequest) error {

	js := `async function postData() {
	var formData = new FormData()
	formData.append('type', 0)
	formData.append('limit', 100)
	formData.append('offset', 0)
	const response = await fetch('https://open.oppomobile.com/resource/list/index.json', {
		method: 'POST',
		body: formData
	})
	return response.text()
}`

	obj, err := page.Evaluate(&rod.EvalOptions{
		ByValue:      true,
		AwaitPromise: true,
		JS:           js,
		UserGesture:  true,
	})
	if err != nil {
		fmt.Println(err)
		return err
	}

	r := gson.NewFrom(obj.Value.Str())

	errno := r.Get("errno").Int()
	if errno != 0 {
		return fmt.Errorf("auth failed. %s", r.Raw())
	}

	var appid string
	for _, row := range r.Get("data").Get("rows").Arr() {
		if row.Get("pkg_name").Str() == req.PackageName {
			appid = row.Get("app_id").Str()
			break
		}
	}

	if len(appid) == 0 {
		return fmt.Errorf("unsupported package. %s", req.PackageName)
	}

	return rod.Try(func() {
		page.MustNavigate(fmt.Sprintf("https://open.oppomobile.com/new/mcom#/home/management/app-admin#/resource/update/index?app_id=%s&is_gray=2", appid))
		iframe := page.MustElement(`iframe[id="menu_service_main_iframe"]`).MustFrame()
		// 判断一下如果有弹窗，先关掉弹窗
		time.Sleep(time.Second * 3)
		exist, noButton, _ := iframe.Has(`#save-the-draft > div > div > div.modal-body > div:nth-child(2) > button.btn.no`)
		if exist {
			noButton.MustClick()
		}

		iframe.MustElement(`textarea[name="update_desc"`).MustSelectAllText().MustInput(req.UpdateDesc)

		wait := page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
			url := e.Response.URL
			if strings.Contains(url, "verify-info.json") {
				m := proto.NetworkGetResponseBody{RequestID: e.RequestID}
				if r, err := m.Call(page); err == nil {
					body := gson.NewFrom(r.Body)
					errno := body.Get("errono").Int()
					if errno == 0 {
						return true
					}

					if errno == 911046 {
						return true
					}

				}
				return false
			}
			return false
		})

		go iframe.MustElement(`#auditphasedbuttonclick`).MustClick()

		wait()
	})
}
