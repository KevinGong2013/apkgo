package oppo

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
	"golang.org/x/net/context"
)

func waitParseAPK(page *rod.Page) error {
	router := page.HijackRequests()
	defer router.MustStop()

	ctx, cancel := context.WithTimeout(context.TODO(), time.Minute*5)
	ch := make(chan *verifyInfoResponse)

	router.MustAdd("*/verify-info.json*", func(ctx *rod.Hijack) {
		// 必不可少的
		ctx.MustLoadResponse()
		resp := new(verifyInfoResponse)
		fmt.Println(ctx.Response.Body())
		if err := json.Unmarshal([]byte(ctx.Response.Body()), resp); err != nil {
			fmt.Printf("unmarshal err %s", err.Error())
			return
		}
		ch <- resp
	})

	go router.Run()

	for {
		select {
		case r := <-ch:
			if r.Errno == 0 {
				cancel()
				return nil
			} else if r.Errno == 911046 {
				cancel()
				return errors.New(r.Data.Message)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

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

	page.Navigate(fmt.Sprintf("https://open.oppomobile.com/new/mcom#/home/management/app-admin#/resource/update/index?app_id=%s&is_gray=2", appid))

	iframe := page.MustElement(`iframe[id="menu_service_main_iframe"]`).MustFrame()

	// 判断一下如果有弹窗，先关掉弹窗
	time.Sleep(time.Second * 3)
	exist, noButton, _ := iframe.Has(`#save-the-draft > div > div > div.modal-body > div:nth-child(2) > button.btn.no`)
	if exist {
		if err := noButton.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return err
		}
	}

	textarea, err := iframe.Element(`textarea[name="update_desc"`)
	if err != nil {
		return err
	}

	// 上传文件
	fmt.Println("上传apk包完成， 等待解析")
	fileUploader, err := iframe.Element(`input[type="file"]`)
	if err != nil {
		return err
	}
	if err := fileUploader.SetFiles([]string{req.ApkFile}); err != nil {
		return err
	}

	err = waitParseAPK(page)
	if err != nil {
		return err
	}

	// 全选文字
	if err := textarea.SelectAllText(); err != nil {
		return err
	}

	// 填写更新说明
	//
	fmt.Println("填写更新日志")
	if err := textarea.Input(req.UpdateDesc); err != nil {
		return err
	}

	btn, err := iframe.Element(`#auditphasedbuttonclick`)
	if err != nil {
		return err
	}

	if err := btn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return err
	}

	fmt.Printf("done. ")

	return nil
}
