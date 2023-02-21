package huawei

import (
	"fmt"
	"path/filepath"

	"github.com/go-resty/resty/v2"
)

// https://developer.huawei.com/consumer/cn/console#/serviceCards/

// 官方文档
// https://developer.huawei.com/consumer/cn/doc/development/AppGallery-connect-Guides/agcapi-getstarted-0000001111845114

// https://developer.huawei.com/consumer/cn/doc/development/AppGallery-connect-References/agcapi-app-info-update-0000001111685198#section62398251

type Client struct {
	restyClient *resty.Client
}

func NewClient(clientId, clientSecret string) (*Client, error) {

	restyClient := resty.New()

	restyClient.SetDebug(false)
	restyClient.SetBaseURL("https://connect-api.cloud.huawei.com")
	restyClient.SetHeader("Content-Type", "application/json")

	// 把Token Bearer
	c := &Client{restyClient: restyClient}

	r, err := c.getToken(clientId, clientSecret)
	if err != nil {
		return nil, err
	}

	if len(r.AccessToken) == 0 {
		return nil, fmt.Errorf("refresh token Failed. %s", r.Reason)
	}

	restyClient.SetAuthToken(r.AccessToken)
	restyClient.SetHeader("client_id", clientId)

	return c, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	Reason      string `json:"ret,omitempty"`
}

func (c *Client) getToken(clientId, clientSecret string) (*tokenResponse, error) {

	resp := new(tokenResponse)
	_, err := c.restyClient.R().
		SetBody(map[string]interface{}{
			"client_id":     clientId,
			"client_secret": clientSecret,
			"grant_type":    "client_credentials",
		}).
		SetResult(resp).
		Post("/api/oauth2/v1/token")
	return resp, err
}

// type updateAppInfoResponse struct {
// 	Code    int    `json:"code"`
// 	Message string `json:"string"`
// }

// releaseType 1 全网 3 分阶段
// appId
// func (c *Client) updateAppInfo(appId string, jsonStr string) error {

// 	var result struct {
// 		Ret updateAppInfoResponse `json:"ret"`
// 	}

// 	_, err := c.restyClient.R().
// 		SetQueryParams(map[string]string{
// 			"releaseType": "1",
// 			"appId":       appId,
// 		}).
// 		SetBody(jsonStr).
// 		SetResult(result).
// 		Put("/api/publish/v2/app-info")
// 	if err != nil {
// 		return err
// 	}

// 	if result.Ret.Code != 0 {
// 		return fmt.Errorf("err: %s", result.Ret.Message)
// 	}

// 	return nil
// }

func (c *Client) fetchAppId(packageName string) (string, error) {
	var r struct {
		Ret    Ret `json:"ret"`
		AppIds []struct {
			Key   string
			Value string
		}
	}
	_, err := c.restyClient.R().
		SetQueryParam("packageName", packageName).
		SetQueryParam("packageTypes", "1").
		SetResult(&r).
		Get("/api/publish/v2/appid-list")
	if err != nil {
		return "", err
	}

	if r.Ret.Code != 0 {
		return "", fmt.Errorf("get appid failed. %s", r.Ret.Message)
	}

	if len(r.AppIds) == 0 {
		return "", fmt.Errorf("%s appId not found", packageName)
	}

	return r.AppIds[0].Value, nil
}

func (c *Client) upload(appId string, apkFilePath string) error {

	// 1. 获取上传apk文件的地址
	var uploadURLResult struct {
		Ret struct {
			Code    int    `json:"code"`
			Message string `json:"msg"`
		} `json:"ret"`
		URL      string `json:"uploadUrl"`
		ChunkURL string `json:"chunkUploadUrl"`
		AuthCode string `json:"authCode"`
	}
	_, err := c.restyClient.R().
		SetQueryParams(map[string]string{
			"releaseType": "1",
			"suffix":      "apk",
			"appId":       appId,
		}).
		SetResult(&uploadURLResult).
		Get("/api/publish/v2/upload-url")
	if err != nil {
		return err
	}
	if len(uploadURLResult.URL) > 0 && uploadURLResult.Ret.Code != 0 {
		return fmt.Errorf("get upload url failed. %s", uploadURLResult.Ret.Message)
	}

	// 上传文件
	var uploadFileResponse struct {
		Result struct {
			UploadFileRsp struct {
				IfSuccess    int `json:"ifSuccess,omitempty"`
				FileInfoList []struct {
					FileDestUlr              string `json:"fileDestUlr,omitempty"`
					Size                     int    `json:"size,omitempty"`
					ImageResolution          string `json:"imageResolution,omitempty"`
					ImageResolutionSingature string `json:"imageResolutionSingature,omitempty"`
				} `json:"fileInfoList,omitempty"`
			} `json:"UploadFileRsp,omitempty"`
			ResultCode string `json:"resultCode"`
		} `json:"result"`
	}

	filename := filepath.Base(apkFilePath)

	_, err = resty.New().R().
		SetFile("file", apkFilePath).
		SetFormData(map[string]string{
			"authCode":  uploadURLResult.AuthCode,
			"fileCount": "1",
			"name":      filename,
			"parseType": "0",
		}).
		SetResult(&uploadFileResponse).
		Post(uploadURLResult.URL)
	if err != nil {
		return err
	}

	if uploadFileResponse.Result.ResultCode != "0" {
		return fmt.Errorf("upload apk failed. resultCode: %s", uploadFileResponse.Result.ResultCode)
	}

	// 更新应用文件信息

	var updateFileInfoResponse struct {
		Ret Ret `json:"ret"`
	}

	_, err = c.restyClient.R().
		SetQueryParams(map[string]string{
			"appId":       appId,
			"releaseType": "1",
		}).
		SetBody(map[string]interface{}{
			"fileType": 5,
			"files": []map[string]interface{}{
				{
					"fileName":    filename,
					"fileDestUrl": uploadFileResponse.Result.UploadFileRsp.FileInfoList[0].FileDestUlr,
				},
			},
		}).
		SetResult(updateFileInfoResponse).
		Put("/api/publish/v2/app-file-info")
	if err != nil {
		return err
	}
	if updateFileInfoResponse.Ret.Code != 0 {
		return fmt.Errorf("update file info failed. %s", updateFileInfoResponse.Ret.Message)
	}

	return nil
}

func (c *Client) submitApp(appId string) *Ret {
	var publishResponse struct {
		Ret Ret `json:"ret"`
	}
	c.restyClient.R().
		SetQueryParams(map[string]string{
			"appId":       appId,
			"releaseType": "1",
		}).
		SetBody(map[string]interface{}{}).
		SetResult(publishResponse).
		Post("/api/publish/v2/app-submit")

	// if publishResponse.Ret.Code != 0 {
	// 	return fmt.Errorf("publish failed. %s", publishResponse.Ret.Message)
	// }

	return &publishResponse.Ret
}

type Ret struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
