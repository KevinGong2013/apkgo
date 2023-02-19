package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/fir"
	"github.com/KevinGong2013/apkgo/cmd/huawei"
	"github.com/KevinGong2013/apkgo/cmd/pgyer"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/vivo"
	"github.com/KevinGong2013/apkgo/cmd/xiaomi"

	"github.com/shogo82148/androidbinary/apk"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

var publishers = make(map[string]shared.Publisher)

func InitialPublishers(filter []string) error {
	cfgFileBytes, err := os.ReadFile(cfgFile)
	if err != nil {
		return err
	}

	config := new(Config)

	if err = json.Unmarshal(cfgFileBytes, config); err != nil {
		return err
	}

	// filters := strings.Join(filter, " ")
	for _, k := range filter {
		v := config.Publishers[k]

		switch k {
		case "xiaomi":
			xm, err := xiaomi.NewClient(v["username"], v["private_key"])
			if err != nil {
				return err
			}
			publishers[k] = xm
		case "vivo":
			vv, err := vivo.NewClient(v["access_key"], v["access_secret"])
			if err != nil {
				return err
			}
			publishers[k] = vv
		case "huawei":
			hw, err := huawei.NewClient(v["client_id"], v["client_secret"])
			if err != nil {
				return err
			}
			publishers[k] = hw
		case "pgyer":
			publishers[k] = pgyer.NewClient(v["api_key"])
		case "fir":
			publishers[k] = fir.NewClient(v["api_token"])
		default:
			// 看看是不是支持的plugin
			if v["magic_cookie_key"] != "" && v["magic_cookie_value"] != "" {

				version, err := strconv.Atoi(v["version"])
				if err != nil {
					return err
				}

				p, err := NewPluginPublisher(&PluginConfig{
					Name:             k,
					Path:             v["path"],
					ProtocolVersion:  uint(version),
					MagicCookieKey:   v["magic_cookie_key"],
					MagicCookieValue: v["magic_cookie_value"],
				})
				if err != nil {
					return nil
				}
				publishers[k] = p
			} else {
				return fmt.Errorf("unsupported market. [%s]", k)
			}
		}
	}

	return nil
}

func Do(updateDesc string, apkFile ...string) error {
	if len(apkFile) == 0 {
		return errors.New("请指定apk文件路径")
	}
	if len(apkFile) > 2 {
		return errors.New("仅支持一个 fat apk，或者 32和64的安装包，不支持多个文件")
	}
	//
	pkg, _ := apk.OpenFile(apkFile[0])

	defer pkg.Close()

	secondApkFile := ""
	if len(apkFile) == 2 {
		secondApkFile = apkFile[1]
	}
	//
	req := shared.PublishRequest{
		AppName:     pkg.Manifest().App.Label.MustString(),
		PackageName: pkg.PackageName(),
		VersionCode: pkg.Manifest().VersionCode.MustInt32(),
		VersionName: pkg.Manifest().VersionName.MustString(),

		ApkFile:       apkFile[0],
		SecondApkFile: secondApkFile,
		UpdateDesc:    updateDesc,
		// 更新
		SynchroType: 1,
	}

	fmt.Printf("apkgo will upload %s\n%s", req.ApkFile, req.SecondApkFile)

	fmt.Println()
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendRow(table.Row{
		fmt.Sprintf("Name: %s\nVersion: %s\nApplicationID: %s",
			text.FgGreen.Sprint(req.AppName),
			text.FgGreen.Sprintf("%s+%d", req.VersionName, req.VersionCode),
			text.FgGreen.Sprint(req.PackageName)),
	})
	t.Render()

	fmt.Println()

	pw := progress.NewWriter()
	pw.SetAutoStop(true)
	pw.SetMessageWidth(24)
	pw.SetTrackerLength(25)
	pw.SetNumTrackersExpected(len(publishers))
	pw.SetSortBy(progress.SortByNone)
	pw.SetStyle(progress.StyleDefault)
	pw.SetTrackerPosition(progress.PositionRight)
	pw.SetUpdateFrequency(time.Millisecond * 100)
	pw.Style().Options.DoneString = "Succeed"
	pw.Style().Options.ErrorString = "Failed"
	pw.Style().Colors = progress.StyleColorsExample
	pw.Style().Visibility.ETA = false
	pw.Style().Visibility.Time = true
	pw.Style().Visibility.Tracker = false
	pw.Style().Visibility.TrackerOverall = true
	pw.Style().Visibility.SpeedOverall = false
	pw.Style().Visibility.Speed = false
	pw.Style().Visibility.Value = false
	pw.Style().Visibility.Percentage = false
	pw.Style().Visibility.Pinned = false

	go pw.Render()

	resultTable := table.NewWriter()

	resultTable.SetOutputMirror(os.Stdout)
	resultTable.AppendHeader(table.Row{"Name", "Result", "Reason"})

	for k := range publishers {
		p := publishers[k]
		go func() {
			tracker := trackPublish(pw, p)
			err := newMockPublisher(p).Do(req)
			if err == nil {
				tracker.MarkAsDone()
				resultTable.AppendRow(table.Row{p.Name(), text.FgGreen.Sprint("Succeed"), "👌"})
			} else {
				tracker.MarkAsErrored()
				resultTable.AppendRow(table.Row{p.Name(), text.FgHiRed.Sprint("Failed"), err.Error()})
			}
		}()
	}

	time.Sleep(time.Second)
	for pw.IsRenderInProgress() {
		time.Sleep(time.Millisecond * 100)
	}

	fmt.Println()
	resultTable.Render()

	// 统计数据

	return nil
}

func trackPublish(pw progress.Writer, publisher shared.Publisher) *progress.Tracker {

	tracker := progress.Tracker{Message: publisher.Name(), ExpectedDuration: time.Minute * 5}

	pw.AppendTracker(&tracker)

	return &tracker
}

// 一下代码主要是测试的时候使用
type mockPublisher struct {
	real shared.Publisher
}

func newMockPublisher(r shared.Publisher) *mockPublisher {
	return &mockPublisher{
		real: r,
	}
}

func (mp *mockPublisher) Name() string {
	return "mock-" + mp.real.Name()
}

func (mp *mockPublisher) Do(req shared.PublishRequest) error {

	r := rand.Intn(10)
	time.Sleep(time.Second * time.Duration(10+r))

	if r%2 == 0 {
		return errors.New("mock publish failed")
	}

	return nil
}
