package xiaomi

import (
	"bytes"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"net/url"
	"os"
	"path/filepath"

	"github.com/KevinGong2013/apkgo/cmd/utils"
	"github.com/go-resty/resty/v2"
	"github.com/shogo82148/androidbinary"
	"github.com/shogo82148/androidbinary/apk"
)

type Client struct {
	restyClient *resty.Client
	userName    string
	pubKey      *rsa.PublicKey
	privateKey  string
}

// https://dev.mi.com/distribute/doc/details?pId=1134
func NewClient(userName string, privateKey string) (*Client, error) {

	publicKey, err := loadPublicKeyFromCert()
	if err != nil {
		return nil, err
	}

	restyClient := resty.New()

	restyClient.SetDebug(true)
	restyClient.SetDebugBodyLimit(2048)
	restyClient.SetBaseURL("http://api.developer.xiaomi.com/devupload")

	c := &Client{
		restyClient: restyClient,
		pubKey:      publicKey,
		userName:    userName,
		privateKey:  privateKey,
	}

	//
	return c, nil
}

// type category struct {
// 	ID   int    `json:"categoryId"`
// 	Name string `json:"categoryName"`
// }

// type categoryResult struct {
// 	Result     int        `json:"result"`
// 	Message    string     `json:"message"`
// 	Categories []category `json:"categories"`
// }

// // 查询小米应用商店的应用分类
// func (c *Client) category() ([]category, error) {

// 	var r categoryResult

// 	_, err := c.restyClient.R().
// 		SetResult(&r).
// 		Post("/dev/category")

// 	return r.Categories, err
// }

type packageInfo struct {
	AppName     string `json:"appName"`
	PackageName string `json:"packageName"`
	VersionCode int    `json:"versionCode"`
	VersionName string `json:"versionName"`
}

// 通过应用包名查询小米应用商店内本账户推送的最新应用详情，用于判断是否需要进行应用推送。
func (c *Client) query(packageName string) (*packageInfo, error) {

	body := c.encode(map[string]interface{}{
		"packageName": packageName,
		"userName":    c.userName,
	}, nil)

	var r struct {
		Result      int          `json:"result"`
		PackageInfo *packageInfo `json:"packageInfo"`
	}
	_, err := c.restyClient.R().
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetBody(body.Encode()).
		SetResult(&r).
		Post("/dev/query")

	return r.PackageInfo, err
}

type appInfoRequest struct {
	AppName       string   `json:"appName"`
	PackageName   string   `json:"packageName"`
	PublisherName *string  `json:"publisherName,omitempty"`
	VersionName   string   `json:"versionName,omitempty"`
	Category      *string  `json:"category,omitempty"`
	KeyWords      *string  `json:"keyWords,omitempty"`
	Desc          *string  `json:"desc,omitempty"`
	UpdateDesc    *string  `json:"updateDesc,omitempty"`
	ShortDesc     *string  `json:"shortDesc,omitempty"`
	Web           *string  `json:"web,omitempty"`
	Price         *float64 `json:"price,omitempty"`
	PrivacyUrl    *string  `json:"privacyUrl,omitempty"`
}

// synchroType 更新类型：0=新增，1=更新包，2=内容更新
func (c *Client) push(synchroType int, apkPath, secondApkPath string, appInfo appInfoRequest) error {

	// 2023/04/27
	//
	// From xiaomi developer:
	// 现阶段麻烦就传现有的图标~某APP去年就因为没有传图标导致图标变为安卓机器人~近期我们又发现了几例这样的事故，所以临时把这个字段做了必填
	// 后续会改成默认读取上一版本的图标

	pkg, _ := apk.OpenFile(apkPath)
	defer pkg.Close()
	icon, err := pkg.Icon(&androidbinary.ResTableConfig{
		Size: 512,
	})
	if err != nil {
		return fmt.Errorf("提取appIcon失败。%s", err.Error())
	}

	iconTempPath := filepath.Join(filepath.Dir(apkPath), "icon.png")
	file, err := os.Create(iconTempPath)
	if err != nil {
		return fmt.Errorf("创建apk icon 文件失败。 %s", err.Error())
	}
	if err := png.Encode(file, icon); err != nil {
		return fmt.Errorf("encode icon file failed. %s", err.Error())
	}

	defer func() {
		// 防止误删
		if len(iconTempPath) > 6 {
			os.Remove(iconTempPath)
		}
	}()

	var jpegIcon bytes.Buffer
	if err := jpeg.Encode(&jpegIcon, icon, &jpeg.Options{Quality: 100}); err != nil {
		return fmt.Errorf("转换图标失败: %s", err.Error())
	}

	// iconReader := bytes.NewReader(jpegIcon.Bytes())

	body := c.encode(map[string]interface{}{
		"synchroType": synchroType,
		"userName":    c.userName,
		"appInfo":     appInfo,
	}, map[string]string{
		"apk":           apkPath,
		"secondApkPath": secondApkPath,
		"icon":          iconTempPath,
	})

	var r struct {
		Result      int          `json:"result"`
		PackageInfo *packageInfo `json:"packageInfo,omitempty"`
		Message     string       `json:"message,omitempty"`
	}

	req := c.restyClient.R().
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetFormDataFromValues(body).
		SetFile("apk", apkPath).
		SetFile("icon", iconTempPath).
		// SetFileReader("icon", "icon.jpeg", iconReader).
		SetResult(&r)
	if len(secondApkPath) > 0 {
		req.SetFile("secondApkPath", secondApkPath)
	}

	_, err = req.Post("/dev/push")

	if err != nil {
		return err
	}

	if r.Result != 0 {
		return errors.New(r.Message)
	}

	return err
}

func (c *Client) encode(params map[string]interface{}, files map[string]string) url.Values {

	requestDataBytes, _ := json.Marshal(params)
	requestDataStr := string(requestDataBytes)

	form := url.Values{}
	form.Add("RequestData", requestDataStr)

	sigItem := make(map[string]string)
	sigItem["name"] = "RequestData"
	md5, _ := utils.MD5(requestDataStr)
	sigItem["hash"] = md5

	sig := make(map[string]interface{})
	sigs := make([]map[string]string, 0)
	sigs = append(sigs, sigItem)

	for key, filepath := range files {
		if len(filepath) > 0 {
			md5, err := utils.FileMD5(filepath)
			if err != nil {
				fmt.Printf("get file md5 failed. %s\n", err)
				return url.Values{}
			}

			sigs = append(sigs, map[string]string{
				"name": key,
				"hash": md5,
			})
		}
	}

	sig["sig"] = sigs
	sig["password"] = c.privateKey

	sigBytes, _ := json.Marshal(sig)

	encryptedSig, _ := encryptByPublicKey(sigBytes, c.pubKey)

	form.Add("SIG", encryptedSig)

	return form
}
