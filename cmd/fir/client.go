package fir

import (
	"github.com/go-resty/resty/v2"
)

type Client struct {
	apiToken    string
	restyClient *resty.Client
}

func NewClient(apiToken string) *Client {

	restyClient := resty.New()

	restyClient.SetDebug(true)
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
