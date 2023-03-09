package publisher

import (
	"fmt"
	"path/filepath"

	"github.com/KevinGong2013/apkgo/cmd/baidu"
	"github.com/KevinGong2013/apkgo/cmd/oppo"
	"github.com/KevinGong2013/apkgo/cmd/qh360"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/tencent"
	"github.com/KevinGong2013/apkgo/cmd/utils"
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

func setupDefaultFlags(l *launcher.Launcher, dir string) *launcher.Launcher {
	return l.
		KeepUserDataDir().
		UserDataDir(dir).
		ProfileDir("apkgo").
		Headless(false).
		Set("disable-gpu").
		Set("disable-features", "OptimizationGuideModelDownloading,OptimizationHintsFetching,OptimizationTargetPrediction,OptimizationHints")
}

func NewBrowserPublisher(identifier string, userDataDir string) (*BrowserPublisher, error) {
	dir := filepath.Join(userDataDir, identifier)
	var b *rod.Browser
	if utils.IsRunningInDockerContainer() {
		l := launcher.MustNewManaged("")
		setupDefaultFlags(l, dir)
		l.XVFB("-a", "--server-args=-screen 0, 1024x768x24")
		b = rod.New().Client(l.MustClient()).MustConnect()
	} else {
		l := launcher.New()
		// l := launcher.MustNewManaged("ws://192.168.3.64:7317")
		setupDefaultFlags(l, dir)
		b = rod.New().ControlURL(l.MustLaunch())
		// b = rod.New().Client(l.MustClient()).MustConnect()
	}

	bd := supportedRods[identifier]
	if bd == nil {
		return nil, fmt.Errorf("unsupported %s", identifier)
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

func (bp *BrowserPublisher) Clean() error {
	return bp.browser.Close()
}
