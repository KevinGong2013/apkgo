package notifiers

import (
	"fmt"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-resty/resty/v2"
)

type Webhook struct {
	Url string
}

func (w *Webhook) Notify(req shared.PublishRequest, result map[string]string) error {

	c := resty.New()
	c.SetRetryCount(3)

	resp, err := c.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("X-APKGO-SIGN", "").
		SetBody(map[string]interface{}{
			"appInfo": req,
			"result":  result,
		}).
		Post(w.Url)
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("err. %d", resp.StatusCode())
	}

	return err
}
