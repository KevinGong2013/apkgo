package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/spf13/cobra"
)

var checkCommand = &cobra.Command{
	Use:   "check",
	Short: "预检查各个平台的授权信息 [todo]",
	Run:   runCheck,
}

func init() {

	rootCmd.AddCommand(checkCommand)
}

func runCheck(cmd *cobra.Command, args []string) {

	dir := filepath.Join(apkgoHome, "chrome-user-data")
	syscall.Umask(0)
	os.MkdirAll(dir, 0755)

	opts := append(chromedp.DefaultExecAllocatorOptions[0:2],
		chromedp.DefaultExecAllocatorOptions[3:]...,
	)
	opts = append(opts, chromedp.Flag("--auto-open-devtools-for-tabs", "true"))

	opts = append(opts, chromedp.UserDataDir(dir))

	ctx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	// also set up a custom logger
	taskCtx, cancel := chromedp.NewContext(ctx, chromedp.WithLogf(log.Printf))
	defer cancel()

	if err := chromedp.Run(taskCtx, chromedp.Navigate("https://open.oppomobile.com/new/ecological/app")); err != nil {
		log.Fatal(err)
		return
	}

	var nodes []*cdp.Node
	if err := chromedp.Run(taskCtx,
		chromedp.WaitReady("body"),
		// 给他一个js的执行时间
		chromedp.Sleep(time.Second*5),
		chromedp.Nodes(".service-item-open", &nodes, chromedp.AtLeast(0)),
	); err != nil {
		log.Fatal(err)
	}

	if len(nodes) > 0 {
		fmt.Println("恭喜已经登陆")
		js := `
async function postData(data = {}) {
  const response = await fetch('https://open.oppomobile.com/resource/list/index.json', {
    method: 'POST',
	body: data
  });
  return response.text();
}; 

let formData = new FormData();
formData.append('type', 0);
formData.append('limit', 100);
formData.append('offset', 0);
formData.append('app_name', '寓小二公寓版');

postData(formData)`
		var response string
		if err := chromedp.Run(taskCtx,
			chromedp.Evaluate(js, &response, func(ep *runtime.EvaluateParams) *runtime.EvaluateParams {
				return ep.WithAwaitPromise(true)
			}),
		); err != nil {
			log.Fatal(err)
		}

		var indexResponse IndexResponse

		if err := json.Unmarshal([]byte(response), &indexResponse); err != nil {
			log.Fatalf("unmarshal json failed %s", err.Error())
			return
		}

		var app OPPOApp

		for _, r := range indexResponse.Data.Rows {
			if r.PkgName == "com.yuxiaor" {
				app = r
				break
			}
		}
		if len(app.PkgName) == 0 {
			// 找不到app
			return
		}

		// 做一些版本处理
		if err := chromedp.Run(taskCtx,
			chromedp.Navigate(fmt.Sprintf("https://open.oppomobile.com/new/mcom#/home/management/app-admin#/resource/update/index?app_id=%s&is_gray=2", app.AppID)),
		); err != nil {
			log.Fatal(err)
		}

		// chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		// 	switch ev := ev.(type) {
		// 	case *page.EventFileChooserOpened:
		// 		go func(backendNodeID cdp.BackendNodeID) {
		// 			if err := chromedp.Run(ctx,
		// 				dom.SetFileInputFiles([]string{"/Users/gix/Downloads/Yuxiaor_arm64-v8a_9572.apk"}).
		// 					WithBackendNodeID(backendNodeID),
		// 			); err != nil {
		// 				log.Fatal(err)
		// 			}
		// 		}(ev.BackendNodeID)
		// 	}
		// })

		if err := chromedp.Run(taskCtx,
			chromedp.SetUploadFiles(`input[type="file"]`, []string{"/Users/gix/Downloads/Yuxiaor_arm64-v8a_9572.apk"}, chromedp.ByQuery),
		); err != nil {
			log.Fatal(err)
			return
		}

		// 监听网络
		ch := make(chan *network.EventResponseReceived)
		chromedp.ListenTarget(taskCtx, func(ev interface{}) {
			if res, ok := ev.(*network.EventResponseReceived); ok {
				ch <- res
			}
		})

		if err := chromedp.Run(taskCtx,
			chromedp.MouseClickNode(nodes[0]),
		); err != nil {
			fmt.Println(err)
			return
		}

	L:
		for {
			select {
			case res := <-ch:
				// print the network request data
				if res.Response.URL == "https://open.oppomobile.com/resource/list/index.json" {
					data, _ := json.Marshal(res.Response)
					fmt.Println(string(data))
					break L
				}

			case <-ctx.Done():
				// context was cancelled
				return
			}
		}

		var trs []*cdp.Node
		if err := chromedp.Run(taskCtx,
			chromedp.Nodes(`table[class*="app-list-v2"] > tbody > tr > td > span[class*="appname"]`, &trs),
		); err != nil {
			log.Fatal(err)
		}

		var appNode *cdp.Node

		appName := "寓小二公寓版"
		for _, tr := range trs {
			if appName == tr.Children[0].NodeValue {
				appNode = tr
				break
			}
		}

		if appNode == nil {
			fmt.Printf("找不到App %s \n", appName)
			return
		}

		if err := chromedp.Run(taskCtx,
			chromedp.MouseClickNode(appNode),
		); err != nil {
			log.Fatal(err)
			return
		}

	} else {
		fmt.Println("登陆过期了请重新登陆吧")
	}

	// 尝试发布

}

type IndexResponse struct {
	Errno int `json:"errno"`
	Data  struct {
		Total int       `json:"total"`
		Rows  []OPPOApp `json:"rows"`
	} `json:"data"`
}

type OPPOApp struct {
	AppID                 string      `json:"app_id"`
	PkgName               string      `json:"pkg_name"`
	Type                  string      `json:"type"`
	Sign                  string      `json:"sign"`
	DevID                 string      `json:"dev_id"`
	AppSecret             string      `json:"app_secret"`
	ServerSecret          string      `json:"server_secret"`
	AppKey                string      `json:"app_key"`
	UpdateTime            string      `json:"update_time"`
	AppCreateTime         string      `json:"app_create_time"`
	SecondCategoryID      string      `json:"second_category_id"`
	ThirdCategoryID       string      `json:"third_category_id"`
	AppName               string      `json:"app_name"`
	IsFreeze              string      `json:"is_freeze"`
	FreezeReason          interface{} `json:"freeze_reason"`
	RefuseReason          string      `json:"refuse_reason"`
	TagList               interface{} `json:"tag_list"`
	IsBusiness            string      `json:"is_business"`
	GameType              string      `json:"game_type"`
	CopyrightURL          string      `json:"copyright_url"`
	SpecialURL            string      `json:"special_url"`
	SpecialFileURL        string      `json:"special_file_url"`
	FreezeFile            interface{} `json:"freeze_file"`
	BusinessUsername      string      `json:"business_username"`
	BusinessEmail         string      `json:"business_email"`
	BusinessMobile        string      `json:"business_mobile"`
	BusinessQq            interface{} `json:"business_qq"`
	BusinessPosition      interface{} `json:"business_position"`
	BusinessAddress       interface{} `json:"business_address"`
	FreezeAdvice          interface{} `json:"freeze_advice"`
	AppType               string      `json:"app_type"`
	AppRealType           string      `json:"app_real_type"`
	AdType                string      `json:"ad_type"`
	DevName               string      `json:"dev_name"`
	ElectronicCertURL     string      `json:"electronic_cert_url"`
	IcpURL                string      `json:"icp_url"`
	RelationAppID         string      `json:"relation_app_id"`
	VersionID             string      `json:"version_id"`
	VersionCode           string      `json:"version_code"`
	VersionName           string      `json:"version_name"`
	ApkURL                string      `json:"apk_url"`
	ApkSize               string      `json:"apk_size"`
	ApkMd5                string      `json:"apk_md5"`
	HeaderMd5             string      `json:"header_md5"`
	Channel               string      `json:"channel"`
	PackagePermission     string      `json:"package_permission"`
	Resolution            interface{} `json:"resolution"`
	VersionType           string      `json:"version_type"`
	CreateTime            string      `json:"create_time"`
	MinSdkVersion         string      `json:"min_sdk_version"`
	TargetSdkVersion      string      `json:"target_sdk_version"`
	VerSecondCategoryID   string      `json:"ver_second_category_id"`
	VerThirdCategoryID    string      `json:"ver_third_category_id"`
	ReleaseType           string      `json:"release_type"`
	ReleaseOverType       string      `json:"release_over_type"`
	PhoneSupport          string      `json:"phone_support"`
	PhoneSupportVersion   string      `json:"phone_support_version"`
	IosLink               string      `json:"ios_link"`
	AdaptiveEquipment     string      `json:"adaptive_equipment"`
	AdaptiveType          string      `json:"adaptive_type"`
	VersionDevice         string      `json:"version_device"`
	ApkFullURL            string      `json:"apk_full_url"`
	OnlineType            string      `json:"online_type"`
	ScheOnlineTime        interface{} `json:"sche_online_time"`
	TestType              string      `json:"test_type"`
	TestStartTime         string      `json:"test_start_time"`
	TestEndTime           string      `json:"test_end_time"`
	PlayerCustomerEmail   interface{} `json:"player_customer_email"`
	PlayerCustomerPhone   interface{} `json:"player_customer_phone"`
	PlayerCustomerQq      interface{} `json:"player_customer_qq"`
	IsSignature           string      `json:"is_signature"`
	IsPreDownload         string      `json:"is_pre_download"`
	CustomerContact       interface{} `json:"customer_contact"`
	Lang                  string      `json:"lang"`
	IconURL               string      `json:"icon_url"`
	IconMd5               string      `json:"icon_md5"`
	Summary               string      `json:"summary"`
	DetailDesc            string      `json:"detail_desc"`
	UpdateDesc            string      `json:"update_desc"`
	AppSubname            string      `json:"app_subname"`
	TestDesc              string      `json:"test_desc"`
	VideoURL              string      `json:"video_url"`
	PicURL                string      `json:"pic_url"`
	PackagePermissionDesc string      `json:"package_permission_desc"`
	VideoPicURL           interface{} `json:"video_pic_url"`
	CoverURL              interface{} `json:"cover_url"`
	LandscapePicURL       string      `json:"landscape_pic_url"`
	PrivacySourceURL      string      `json:"privacy_source_url"`
	ReleaseDesc           string      `json:"release_desc"`
	TestURL               string      `json:"test_url"`
	EnglishName           string      `json:"english_name"`
	Region                string      `json:"region"`
	Level                 string      `json:"level"`
	State                 string      `json:"state"`
	AuditStatus           string      `json:"audit_status"`
	OnlineTime            string      `json:"online_time"`
	OfflineTime           string      `json:"offline_time"`
	IsFirstPublish        string      `json:"is_first_publish"`
	BusinessRefuseReason  interface{} `json:"business_refuse_reason"`
	OldAuditStatus        string      `json:"old_audit_status"`
	ReleaseStatus         string      `json:"release_status"`
	AgeLevel              string      `json:"age_level"`
	NotCompatibleNum      int         `json:"not_compatible_num"`
	Shield                []struct {
		CompatTestID string `json:"compat_test_id"`
		ModelName    string `json:"model_name"`
		ReportURL    string `json:"report_url"`
		State        string `json:"state"`
		ErrorCode    string `json:"error_code"`
		MarketName   string `json:"market_name"`
	} `json:"shield,omitempty"`
	ChangeState            string `json:"change_state"`
	OnlineInfoOfflineApply []struct {
		AppID       string `json:"app_id"`
		VersionID   string `json:"version_id"`
		OnlineState string `json:"online_state"`
		OnlineTime  string `json:"online_time"`
		OfflineTime string `json:"offline_time"`
	} `json:"online_info_offline_apply"`
	FirstOnlineTime         string        `json:"first_online_time"`
	OnlineLevelTag          string        `json:"online_level_tag"`
	Size                    string        `json:"size"`
	AuditStatusName         string        `json:"audit_status_name"`
	OfflineInfo             interface{}   `json:"offline_info,omitempty"`
	TransferState           int           `json:"transfer_state"`
	UpdateInfoCheck         int           `json:"update_info_check"`
	LevelTag                string        `json:"level_tag"`
	RefuseAdvice            string        `json:"refuse_advice"`
	RefuseFile              string        `json:"refuse_file"`
	LevelReason             string        `json:"level_reason"`
	LevelFile               string        `json:"level_file"`
	Revoke                  int           `json:"revoke"`
	IsHasReserve            int           `json:"is_has_reserve"`
	ReserveState            int           `json:"reserve_state"`
	LandscapePicURLMaterial []interface{} `json:"landscape_pic_url_material"`
	PicURLMaterial          []struct {
		URL    string `json:"url"`
		Width  string `json:"width"`
		Height string `json:"height"`
		Md5    string `json:"md5"`
		Size   string `json:"size"`
	} `json:"pic_url_material"`
	VideoURLMaterial []interface{} `json:"video_url_material"`
	PkgSymbol        int           `json:"pkg_symbol"`
	IsReleasePkg     int           `json:"is_release_pkg"`
}
