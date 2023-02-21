package fir

import (
	"fmt"

	"github.com/go-resty/resty/v2"
)

type Client struct {
	apiToken    string
	restyClient *resty.Client
}

func NewClient(apiToken string) *Client {

	restyClient := resty.New()

	restyClient.SetDebug(false)
	restyClient.SetDebugBodyLimit(1000)

	restyClient.SetBaseURL("http://api.bq04.com")

	return &Client{
		apiToken:    apiToken,
		restyClient: restyClient,
	}
}

type getUploadTokenResponse struct {
	Message string `json:"message,omitempty"`
	ID      string `json:"id"`
	Type    string `json:"type"`
	Short   string `json:"short"`
	Cert    struct {
		Icon struct {
			Key       string `json:"key"`
			Token     string `json:"token"`
			UploadURL string `json:"upload_url"`
		} `json:"icon"`
		Binary struct {
			Key       string `json:"key"`
			Token     string `json:"token"`
			UploadURL string `json:"upload_url"`
		} `json:"binary"`
	} `json:"cert"`
}

func (c *Client) getUploadToken(packageName string) (*getUploadTokenResponse, error) {

	var r getUploadTokenResponse

	_, err := c.restyClient.R().
		SetFormData(map[string]string{
			"type":      "android",
			"bundle_id": packageName,
			"api_token": c.apiToken,
		}).
		SetResult(&r).
		Post("/apps")

	return &r, err
}

type getAppInfoResponse struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Changelog    string `json:"changelog"`
	VersionShort string `json:"versionShort"`
	Build        string `json:"build"`
	InstallURL   string `json:"installUrl"`
	InstallURL0  string `json:"install_url"`
	UpdateURL    string `json:"update_url"`
	Binary       struct {
		Fsize int `json:"fsize"`
	} `json:"binary"`
}

func (c *Client) getAppInfo(packageName string) (*getAppInfoResponse, error) {

	r := new(getAppInfoResponse)

	_, err := c.restyClient.R().
		SetQueryParams(map[string]string{
			"type":      "android",
			"api_token": c.apiToken,
		}).
		SetResult(r).
		Get(fmt.Sprintf("/apps/latest/%s", packageName))

	return r, err

}
