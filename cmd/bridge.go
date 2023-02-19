package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

	for _, f := range filter {
		if v, ok := config.Publishers[f]; ok {
			switch f {
			case "xiaomi":
				xm, err := xiaomi.NewClient(v["username"], v["private_key"])
				if err != nil {
					return err
				}
				publishers[f] = xm
			case "vivo":
				vv, err := vivo.NewClient(v["access_key"], v["access_secret"])
				if err != nil {
					return err
				}
				publishers[f] = vv
			case "huawei":
				hw, err := huawei.NewClient(v["client_id"], v["client_secret"])
				if err != nil {
					return err
				}
				publishers[f] = hw
			case "pgyer":
				publishers[f] = pgyer.NewClient(v["api_key"])
			case "fir":
				publishers[f] = fir.NewClient(v["api_token"])
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
			err := shared.NewMockPublisher(p).Do(req)
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
	// ticker := time.Tick(time.Millisecond * 500)
	// updateTicker := time.Tick(time.Millisecond * 250)
	// for !tracker.IsDone() {
	// 	select {
	// 	case <-ticker:
	// 		tracker.Increment(incrementPerCycle)
	// 		if idx == int64(*flagNumTrackers) && tracker.Value() >= total {
	// 			tracker.MarkAsDone()
	// 		} else if *flagRandomFail && rand.Float64() < 0.1 {
	// 			tracker.MarkAsErrored()
	// 		}
	// 		pw.SetPinnedMessages(
	// 			fmt.Sprintf(">> Current Time: %-32s", time.Now().Format(time.RFC3339)),
	// 			fmt.Sprintf(">>   Total Time: %-32s", time.Now().Sub(timeStart).Round(time.Millisecond)),
	// 		)
	// 	case <-updateTicker:
	// 		if updateMessage {
	// 			rndIdx := rand.Intn(len(messageColors))
	// 			if rndIdx == len(messageColors) {
	// 				rndIdx--
	// 			}
	// 			tracker.UpdateMessage(messageColors[rndIdx].Sprint(message))
	// 		}
	// 	}
	// }
}
