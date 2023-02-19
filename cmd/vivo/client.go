package vivo

import (
	"encoding/json"
	"fmt"

	"github.com/KevinGong2013/apkgo/cmd/utils"
	"github.com/go-resty/resty/v2"
)

// https://dev.vivo.com.cn/documentCenter/doc/327
type Client struct {
	restyClient       *resty.Client
	accessKey         string
	accessSecretBytes []byte
}

func NewClient(accessKey, accessSecret string) (*Client, error) {

	restyClient := resty.New()

	restyClient.SetDebug(true)
	restyClient.SetDebugBodyLimit(1000)

	restyClient.SetBaseURL("https://developer-api.vivo.com.cn/router/rest")
	restyClient.SetHeader("Content-Type", "application/json")

	// 把Token Bearer
	c := &Client{restyClient: restyClient, accessKey: accessKey, accessSecretBytes: []byte(accessSecret)}

	return c, nil
}

// 应用创建，更新64位apk包上传
func (c *Client) upload32(packageName, apkFilepath string) (*appInfoResponse, error) {
	return c.upload("app.upload.apk.app.32", packageName, apkFilepath)
}

// 应用创建，更新32位apk包上传
func (c *Client) upload64(packageName, apkFilepath string) (*appInfoResponse, error) {
	return c.upload("app.upload.apk.app.64", packageName, apkFilepath)
}

// 应用创建，更新apk包上传
func (c *Client) uploadFlat(packageName, apkFilepath string) (*appInfoResponse, error) {
	return c.upload("app.upload.apk.app", packageName, apkFilepath)
}

func (c *Client) upload(method, packageName, apkFilepath string) (*appInfoResponse, error) {
	fileMd5, err := utils.FileMD5(apkFilepath)
	if err != nil {
		return nil, err
	}

	params := c.assembleParams(method, map[string]string{
		"packageName": packageName,
		"fileMd5":     fileMd5,
	})

	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    *appInfoResponse
	}

	_, err = c.restyClient.R().
		SetFile("file", apkFilepath).
		SetQueryParams(params).
		SetResult(&r).
		Post("")

	if err != nil {
		return nil, err
	}

	if r.Code != 0 {
		return nil, fmt.Errorf("upload apk failed. %s", r.Message)
	}

	return r.Data, nil
}

func (c *Client) updateApp(apk, fileMd5 string, req updateAppRequest) error {
	return c.update("app.sync.update.app", req, map[string]string{
		"apk":     apk,
		"fileMd5": fileMd5,
	})
}

func (c *Client) updateSubPackageApp(apk32, apk64 string, req updateAppRequest) error {
	return c.update("app.sync.update.subpackage.app", req, map[string]string{
		"apk32": apk32,
		"apk64": apk64,
	})
}

func (c *Client) update(method string, req updateAppRequest, additions map[string]string) error {
	jsonBytes, err := json.Marshal(req)
	if err != nil {
		return err
	}

	var result map[string]string
	err = json.Unmarshal(jsonBytes, &result)
	if err != nil {
		return err
	}

	for k, v := range additions {
		result[k] = v
	}

	params := c.assembleParams(method, result)

	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	c.restyClient.R().
		SetQueryParams(params).
		SetResult(&r).
		Post("")

	if r.Code != 0 {
		return fmt.Errorf("public new version failed. %s", r.Message)
	}

	return nil
}

type appInfoResponse struct {
	PackageName  string `json:"packageName"`
	SerialNumber string `json:"serialnumber"`
	VersionCode  int    `json:"versionCode"`
	VersionName  string `json:"versionName"`
	FileMd5      string `json:"fileMd5"`
}

type updateAppRequest struct {
	PackageName           string `json:"packageName"`
	VersionCode           string `json:"versionCode"`
	OnlineType            string `json:"onlineType"`
	UpdateDesc            string `json:"updateDesc,omitempty"`
	DetailDesc            string `json:"detailDesc,omitempty"`
	Icon                  string `json:"icon,omitempty"`
	Screenshot            string `json:"screenshot,omitempty"`
	ScheOnlineTime        string `json:"scheOnlineTime,omitempty"`
	MainTitle             string `json:"mainTitle,omitempty"`
	SubTitle              string `json:"subTitle,omitempty"`
	AppClassify           string `json:"appClassify,omitempty"`
	SubAppClassify        string `json:"subAppClassify,omitempty"`
	Remark                string `json:"remark,omitempty"`
	SpecialQualifications string `json:"specialQualifications,omitempty"`
	ECopyright            string `json:"ecopyright,omitempty"`
	Safetyreport          string `json:"safetyreport,omitempty"`
	NetworkCultureLicense string `json:"networkCultureLicense,omitempty"`
	PrivateSelfCheck      string `json:"privateSelfCheck"`
	CopyrightList         string `json:"copyrightList,omitempty"`
	CompatibleDevice      string `json:"compatibleDevice"`
	CustomerService       string `json:"customerService,omitempty"`
	SimpleDesc            string `json:"simpleDesc,omitempty"`
	IcpNumber             string `json:"icpNumber,omitempty"`
}
