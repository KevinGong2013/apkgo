package oppo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

// REF: https://openfs.oppomobile.com/open/oop/openapi/demo/demo.zip

// 计算签名
func calSign(key string, data url.Values) string {
	// 将请求参数按key排序
	keys_arr := make([]string, 0, len(data))
	for key := range data {
		keys_arr = append(keys_arr, key)
	}
	sort.Strings(keys_arr)
	// 拼接参数
	sign_arr := make([]string, 0, len(keys_arr))
	for _, key := range keys_arr {
		sign := key + "=" + data.Get(key)
		sign_arr = append(sign_arr, sign)
	}
	sign_str := strings.Join(sign_arr, "&")

	// 进行HmacSHA256计算
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(sign_str))
	res := h.Sum(nil)
	return hex.EncodeToString(res)
}
