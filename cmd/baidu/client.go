package baidu

import (
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-rod/rod"
)

type BaiduClient string

var DefaultClient = BaiduClient("baidu")

func (bc BaiduClient) Identifier() string {
	return string(bc)
}

func (bc BaiduClient) Name() string {
	return "百度移动应用平台"
}

func (bc BaiduClient) CheckAuth(browser *rod.Browser, reAuth bool) (*rod.Page, error) {
	return nil, nil
}

func (bc BaiduClient) Do(page *rod.Page, req shared.PublishRequest) error {
	return nil
}
