package qh360

import (
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-rod/rod"
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
	return nil, nil
}

func (qc QH360Client) Do(page *rod.Page, req shared.PublishRequest) error {
	return nil
}
