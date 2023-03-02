package tencent

import (
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-rod/rod"
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
	return nil, nil
}

func (tc TencentClient) Do(page *rod.Page, req shared.PublishRequest) error {
	return nil
}
