package utils

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

type NetRequestHandler func(body gson.JSON) (stop bool, err error)

func WaitRequest(page *rod.Page, httpPath string, exec func(), handler NetRequestHandler) error {

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	quit := make(chan error)

	go page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		if strings.Contains(e.Response.URL, httpPath) {
			m := proto.NetworkGetResponseBody{RequestID: e.RequestID}
			r, err := m.Call(page)
			if err != nil {
				fmt.Printf("fetch response %s failed \n", httpPath)
			} else {
				stop, err := handler(gson.NewFrom(r.Body))
				// 已经拿到想要的数据了 退出
				if stop {
					quit <- err
				}
				return stop
			}
		}
		return false
	})()

	exec()

	select {
	case err := <-quit:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
