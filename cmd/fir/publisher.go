package fir

import (
	"fmt"
	"strconv"

	"github.com/KevinGong2013/apkgo/cmd/shared"
)

func (c *Client) Do(req shared.PublishRequest) error {

	resp, err := c.getUploadToken(req.PackageName)
	if err != nil {
		return err
	}

	if len(resp.ID) == 0 {
		return fmt.Errorf("err: %s", resp.Message)
	}

	var response struct {
		Completed bool `json:"is_completed"`
	}

	r, err := c.restyClient.R().
		SetFormData(map[string]string{
			"key":         resp.Cert.Binary.Key,
			"token":       resp.Cert.Binary.Token,
			"x:name":      req.AppName,
			"x:version":   req.VersionName,
			"x:build":     strconv.Itoa(int(req.VersionCode)),
			"x:changelog": req.UpdateDesc,
		}).
		SetFile("file", req.ApkFile).
		SetResult(&response).
		Post(resp.Cert.Binary.UploadURL)
	if err != nil {
		return err
	}

	if !response.Completed {
		return fmt.Errorf("upload failed. %s", string(r.Body()))
	}

	return nil
}
