package oppo

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

// https://open.oppomobile.com/new/developmentDoc/info?id=10998

// 1300699476073808945
// 2ebf891579110cfdd7d1095adab056f716c1ecda029132f98bde73b00d220b31

type Client struct {
	restyClient  *resty.Client
	accessToken  string
	clientSecret string
}

func (c *Client) Name() string {
	return "oppo开放平台"
}

func NewClient(clientId, clientSecret string) (*Client, error) {

	restyClient := resty.New()

	restyClient.SetDebugBodyLimit(10000)

	restyClient.SetBaseURL("https://oop-openapi-cn.heytapmobi.com")
	restyClient.SetHeader("Content-Type", "application/json")
	restyClient.SetDebug(true)

	// 获取token
	var result struct {
		Errno int `json:"errno"`
		Data  struct {
			AccessToken string `json:"access_token"`
			ExpireIn    int    `json:"expire_in"`
		} `json:"data"`
	}
	_, err := restyClient.R().SetResult(&result).SetQueryParams(map[string]string{
		"client_id":     clientId,
		"client_secret": clientSecret,
	}).Get("/developer/v1/token")
	if err != nil {
		return nil, err
	}

	token := result.Data.AccessToken

	c := &Client{restyClient: restyClient, accessToken: token, clientSecret: clientSecret}

	return c, nil
}

// 签名
func (c *Client) sign(data url.Values) url.Values {
	// 公共参数
	data.Set("access_token", c.accessToken)
	data.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	// 计算签名
	api_sign := calSign(c.clientSecret, data)
	data.Set("api_sign", api_sign)

	return data
}

// 查询普通包详情
func (c *Client) query(pkgName string) (*app, error) {
	data := make(url.Values)
	data.Set("pkg_name", pkgName)
	var result struct {
		Errno int  `json:"errno"`
		Data  *app `json:"data"`
	}
	if _, err := c.restyClient.R().SetResult(&result).
		SetQueryParamsFromValues(c.sign(data)).Get("/resource/v1/app/info"); err != nil {
		return nil, err
	}
	if result.Errno != 0 {
		return nil, fmt.Errorf("查询失败基础信息失败 code: %d", result.Errno)
	}

	return result.Data, nil
}

// 上传文件
func (c *Client) uploadAPK(file string) (*uploadResult, error) {
	var result struct {
		Errno int `json:"errno"`
		Data  struct {
			UploadUrl string `json:"upload_url"`
			Sign      string `json:"sign"`
		} `json:"data"`
	}
	// 1. 获取上传url
	if _, err := c.restyClient.R().
		SetResult(&result).
		SetQueryParamsFromValues(c.sign(make(url.Values))).
		Get("/resource/v1/upload/get-upload-url"); err != nil {
		return nil, err
	}

	var uploadResult struct {
		Errno int          `json:"errno"`
		Data  uploadResult `json:"data"`
	}

	if _, err := c.restyClient.R().SetResult(&uploadResult).
		SetFormData(map[string]string{
			"sign": result.Data.Sign,
			"type": "apk",
		}).
		SetFile("file", file).
		Post(result.Data.UploadUrl); err != nil {
		return nil, err
	}

	return &uploadResult.Data, nil
}

func (c *Client) publish(req publishRequestParameter) error {

	var result struct {
		Errno int `json:"errno"`
		Data  struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			LogId   int    `json:"logid"`
		} `json:"data"`
	}

	if _, err := c.restyClient.R().
		SetResult(&result).
		SetFormDataFromValues(c.sign(req.toValues())).
		Post("/resource/v1/app/upd"); err != nil {
		return err
	}
	if result.Errno != 0 {
		return fmt.Errorf("发布失败 message: %s logid: %d", result.Data.Message, result.Data.LogId)
	}

	return nil
}

// 获取任务状态

func (c *Client) taskState(packageName string, versionCode string) (*taskBody, error) {
	data := make(url.Values)
	data.Set("pkg_name", packageName)
	data.Set("version_code", versionCode)
	var result struct {
		Errno int       `json:"errno"`
		Data  *taskBody `json:"data"`
	}
	if _, err := c.restyClient.R().SetResult(&result).
		SetFormDataFromValues(c.sign(data)).
		Post("/resource/v1/app/task-state"); err != nil {
		return nil, err
	}
	if result.Errno != 0 {
		return nil, fmt.Errorf("查询任务状态失败 code: %d", result.Errno)
	}

	return result.Data, nil
}
