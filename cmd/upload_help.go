package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/notifiers"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/shogo82148/androidbinary/apk"
	"github.com/spf13/cobra"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

func assemblePublishRequest(cmd *cobra.Command) shared.PublishRequest {

	file, _ := cmd.Flags().GetString("file")
	file32, _ := cmd.Flags().GetString("file32")
	file64, _ := cmd.Flags().GetString("file64")
	stores, _ := cmd.Flags().GetStringSlice("store")
	releaseNots, _ := cmd.Flags().GetString("release-notes")

	apkFile := file
	splitPackage := false
	if len(apkFile) == 0 {
		apkFile = file32
		splitPackage = true
	}

	// 解析apk文件
	pkg, _ := apk.OpenFile(apkFile)
	defer pkg.Close()
	//
	req := shared.PublishRequest{
		AppName:     pkg.Manifest().App.Label.MustString(),
		PackageName: pkg.PackageName(),
		VersionCode: pkg.Manifest().VersionCode.MustInt32(),
		VersionName: pkg.Manifest().VersionName.MustString(),

		ApkFile:       file,
		SecondApkFile: file64,
		UpdateDesc:    releaseNots,
		// 更新
		SynchroType: 1,
		Stores:      strings.Join(stores, ","),
	}
	if splitPackage {
		req.ApkFile = file32
	}

	return req
}

func publish(req shared.PublishRequest, ps []shared.Publisher) map[string]string {

	pw := progress.NewWriter()
	pw.SetAutoStop(true)
	pw.SetMessageWidth(24)
	pw.SetTrackerLength(25)
	pw.SetNumTrackersExpected(len(ps))
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

	result := make(map[string]string)

	for k := range ps {
		p := ps[k]
		name := p.Name()
		go func() {
			tracker := trackPublish(pw, p)
			err := p.Do(req)

			if err == nil {
				tracker.MarkAsDone()
				resultTable.AppendRow(table.Row{name, text.FgGreen.Sprint("Succeed"), "👌"})
				result[name] = ""
			} else {
				tracker.MarkAsErrored()
				resultTable.AppendRow(table.Row{name, text.FgRed.Sprint("Failed"), err.Error()})
				result[name] = err.Error()
			}
		}()
	}

	time.Sleep(time.Second)
	for pw.IsRenderInProgress() {
		time.Sleep(time.Millisecond * 100)
	}

	fmt.Println()
	resultTable.Render()

	return result
}

func trackPublish(pw progress.Writer, publisher shared.Publisher) *progress.Tracker {

	tracker := progress.Tracker{Message: publisher.Name(), ExpectedDuration: time.Minute * 5}

	pw.AppendTracker(&tracker)

	return &tracker
}

func notify(config *StoreConfig, req shared.PublishRequest, result map[string]string) error {

	if config.Notifiers.Lark != nil {
		l := &notifiers.LarkNotifier{
			Key:         config.Notifiers.Lark.Key,
			SecretToken: config.Notifiers.Lark.SecretToken,
		}
		if err := l.Notify(l.BuildAppPubishedMessage(req, result)); err != nil {
			return err
		}
	}

	if config.Notifiers.Dingtalk != nil {
		dt := notifiers.DingTalkNotifier{
			AccessToken: config.Notifiers.Dingtalk.AccessToken,
			SecretToken: config.Notifiers.Dingtalk.SecretToken,
		}
		if err := dt.Notify(dt.BuildAppPubishedMessage(req, result)); err != nil {
			return err
		}
	}

	if config.Notifiers.Wecom != nil {
		w := notifiers.WeComNotifier{
			Key: config.Notifiers.Wecom.Key,
		}
		if err := w.Notify(w.BuildAppPubishedMessage(req, result)); err != nil {
			return err
		}
	}

	if config.Notifiers.Webhook != nil {
		w := notifiers.Webhook{Url: config.Notifiers.Webhook.Url}
		if err := w.Notify(req, result); err != nil {
			return err
		}
	}

	return nil
}
