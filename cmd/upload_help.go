package cmd

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/fir"
	"github.com/KevinGong2013/apkgo/cmd/huawei"
	"github.com/KevinGong2013/apkgo/cmd/notifiers"
	"github.com/KevinGong2013/apkgo/cmd/pgyer"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/vivo"
	"github.com/KevinGong2013/apkgo/cmd/xiaomi"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

var publishers = make(map[string]shared.Publisher)

func initialPublishers() error {

	for _, k := range stores {
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

func publish(req shared.PublishRequest) map[string]string {

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

	result := make(map[string]string)

	for k := range publishers {
		p := publishers[k]
		name := p.Name()
		go func() {
			tracker := trackPublish(pw, p)
			err := newMockPublisher(p).Do(req)

			if err == nil {
				tracker.MarkAsDone()
				resultTable.AppendRow(table.Row{name, text.FgGreen.Sprint("Succeed"), "👌"})
				result[name] = ""
			} else {
				tracker.MarkAsErrored()
				resultTable.AppendRow(table.Row{name, text.FgHiRed.Sprint("Failed"), err.Error()})
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

func notify(req shared.PublishRequest, result map[string]string) error {

	builder := new(strings.Builder)
	var failed []string
	for k, v := range result {
		if len(v) == 0 {
			builder.WriteString(fmt.Sprintf("👌%s上传成功\n", k))
		} else {
			builder.WriteString(fmt.Sprintf("❌%s err: %s\n", k, v))
			failed = append(failed, k)
		}
	}
	if len(failed) == 0 {
		builder.WriteString("👏👏👏 所有平台上传成功")
	} else if len(failed) == len(result) {
		builder.WriteString("😢😢😢 所有平台上传失败")
	} else {
		builder.WriteString(fmt.Sprintf("%s 上传失败，请检查", strings.Join(failed, ",")))
	}

	if config.Notifiers.Lark != nil {
		l := &notifiers.LarkNotifier{
			Key:         config.Notifiers.Lark.Key,
			SecretToken: config.Notifiers.Lark.SecretToken,
		}
		if err := l.Notify(l.BuildAppPubishedMessage(req, builder.String(), "customMsg")); err != nil {
			return err
		}
	}

	if config.Notifiers.WebHook != nil {
		w := notifiers.Webhook{Url: config.Notifiers.WebHook.Url}
		if err := w.Notify(req, result); err != nil {
			return err
		}
	}

	fmt.Println(builder.String())

	return nil
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
	time.Sleep(time.Second * time.Duration(r))

	if r%2 == 0 {
		return errors.New("mock publish failed")
	}

	return nil
}
