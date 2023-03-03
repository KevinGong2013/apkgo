package oppo

import (
	"fmt"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/utils"
	"github.com/go-rod/rod"
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

		if err := utils.WaitRequest(page, "verify-info.json", func() {
			iframe.MustElement(`input[type="file"]`).MustInput(req.ApkFile)
		}, func(body gson.JSON) (stop bool, err error) {
			errno := body.Get("errono").Int()
			if errno == 0 {
				return true, nil
			}

			if errno == 911046 {
				return true, fmt.Errorf("err: %s", body.String())
			}

			return false, nil
		}); err != nil {
			panic(err)
		}

		iframe.MustElement(`#auditphasedbuttonclick`).MustClick()
	})
}
