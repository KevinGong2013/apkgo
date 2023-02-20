package pgyer

import (
	"github.com/go-resty/resty/v2"
)

type Client struct {
	apiKey      string
	restyClient *resty.Client
}

// https://www.pgyer.com/doc/view/api#fastUploadApp
func NewClient(apiKey string) *Client {

	restyClient := resty.New()

	restyClient.SetDebug(true)
	restyClient.SetDebugBodyLimit(1000)

	restyClient.SetBaseURL("https://www.pgyer.com/apiv2")
	restyClient.SetHeader("Content-Type", "application/x-www-form-urlencoded")

	return &Client{
		apiKey:      apiKey,
		restyClient: restyClient,
	}
}

type getCosTokenResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Key      string            `json:"key"`
		Endpoint string            `json:"endpoint"`
		Params   map[string]string `json:"params"`
	} `json:"data"`
}

func (c *Client) getCosToken(updateDesc string) (*getCosTokenResponse, error) {

	var r getCosTokenResponse

	_, err := c.restyClient.R().
		SetFormData(map[string]string{
			"_api_key":               c.apiKey,
			"buildType":              "apk",
			"buildUpdateDescription": updateDesc,
		}).
		SetResult(&r).
		Post("/app/getCOSToken")

	return &r, err
}

type buildInfoResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Key          string `json:"buildKey"`
		Type         int    `json:"buildType"`
		IsFirst      int    `json:"buildIsFirst"`
		IsLastest    int    `json:"buildIsLastest"`
		FileSize     int    `json:"buildFileSize"`
		Name         string `json:"buildName"`
		Version      string `json:"buildVersion"`
		VersionNo    string `json:"buildVersionNo"`
		BuildVersion int    `json:"buildBuildVersion"`
		Identifier   string `json:"buildIdentifier"`
		Icon         string `json:"buildIcon"`
		Description  string `json:"buildDescription"`
		UpdateDesc   string `json:"buildUpdateDescription"`
		ScreenShots  string `json:"buildScreenShots"`
		ShortcutUrl  string `json:"buildShortcutUrl"`
		QRCodeUrl    string `json:"buildQRCodeURL"`
		Created      string `json:"buildCreated"`
		Updated      string `json:"buildUpdated"`
	} `json:"data"`
}

func (c *Client) buildInfo(buildKey string) (buildInfoResponse, error) {

	var r buildInfoResponse

	_, err := c.restyClient.R().
		SetQueryParams(map[string]string{
			"_api_key": c.apiKey,
			"buildKey": buildKey,
		}).
		SetResult(&r).
		Get("/app/buildInfo")

	return r, err
}
