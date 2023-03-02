package oppo

import (
	"errors"
	"fmt"

	"github.com/go-rod/rod"
)

func (c *Client) check(reAuth bool) error {
	_, err := c.page.Race().ElementR("h1", "Sign in").Handle(func(e *rod.Element) error {
		if !reAuth {
			return errors.New("登陆态失效")
		}
		fmt.Println("登录用户登陆...")
		if _, err := c.page.Eval("(msg) => { alert(msg) }", "登录完成以后会自动同步到apkgo"); err != nil {
			return err
		}
		return c.page.WaitElementsMoreThan(".service-item-open", 0)
	}).Element(".service-item-open").MustHandle(func(e *rod.Element) {
		fmt.Println("已经登陆成功，免登")
	}).Do()
	return err
}
