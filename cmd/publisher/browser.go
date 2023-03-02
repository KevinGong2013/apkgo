package publisher

import (
	"fmt"
	"path/filepath"

	"github.com/KevinGong2013/apkgo/cmd/baidu"
	"github.com/KevinGong2013/apkgo/cmd/oppo"
	"github.com/KevinGong2013/apkgo/cmd/qh360"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/tencent"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

type browserRod interface {
	Identifier() string
	Name() string
	CheckAuth(browser *rod.Browser, reAuth bool) (*rod.Page, error)
	Do(page *rod.Page, req shared.PublishRequest) error
}

var supportedRods = map[string]browserRod{
	oppo.DefaultClient.Identifier():    oppo.DefaultClient,
	tencent.DefaultClient.Identifier(): tencent.DefaultClient,
	baidu.DefaultClient.Identifier():   baidu.DefaultClient,
	qh360.DefaultClient.Identifier():   qh360.DefaultClient,
}

type BrowserPublisher struct {
	id         string
	browser    *rod.Browser
	browserRod browserRod
}

func NewBrowserPublisher(identifier string, userDataDir string) (*BrowserPublisher, error) {
	u, err := launcher.New().
		UserDataDir(filepath.Join(userDataDir, identifier)).
		ProfileDir("apkgo").
		Headless(false).
		Set("disable-gpu").
		Set("disable-features", "OptimizationGuideModelDownloading,OptimizationHintsFetching,OptimizationTargetPrediction,OptimizationHints").
		Launch()
	if err != nil {
		return nil, err
	}

	b := rod.New().ControlURL(u)

	// if err := b.Connect(); err != nil {
	// 	return nil, err
	// }

	bd := supportedRods[identifier]
	if bd == nil {
		return nil, fmt.Errorf("unsupport %s", identifier)
	}

	if err := b.Connect(); err != nil {
		return nil, err
	}

	return &BrowserPublisher{
		id:         identifier,
		browser:    b,
		browserRod: bd,
	}, nil
}

func (bp *BrowserPublisher) Name() string {
	return bp.browserRod.Name()
}

func (bp *BrowserPublisher) Do(req shared.PublishRequest) error {
	page, err := bp.browserRod.CheckAuth(bp.browser, false)
	if err != nil {
		return fmt.Errorf("%s 认证失败请执行 apkgo check %s", bp.Name(), bp.id)
	}
	defer page.Close()

	return bp.browserRod.Do(page, req)
}

func (bp *BrowserPublisher) CheckAuth(reAuth bool) error {
	_, err := bp.browserRod.CheckAuth(bp.browser, reAuth)
	return err
}

func (bp *BrowserPublisher) PostDo() error {
	return bp.browser.Close()
}
