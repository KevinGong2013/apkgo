package publisher

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/fir"
	"github.com/KevinGong2013/apkgo/cmd/huawei"
	"github.com/KevinGong2013/apkgo/cmd/pgyer"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/vivo"
	"github.com/KevinGong2013/apkgo/cmd/xiaomi"
)

func NewCurlClient(name, key, secret string) (shared.Publisher, error) {
	switch name {
	case "huawei":
		return huawei.NewClient(key, secret)
	case "xiaomi":
		return xiaomi.NewClient(key, secret)
	case "vivo":
		return vivo.NewClient(key, secret)
	case "pgyer":
		return pgyer.NewClient(key), nil
	case "fir":
		return fir.NewClient(key), nil
	default:
		return &mockPublisher{
			name:   name,
			key:    key,
			secret: secret,
		}, nil
	}
}

// 以下代码主要是测试的时候使用
type mockPublisher struct {
	name   string
	key    string
	secret string
}

func (mp *mockPublisher) Name() string {
	return fmt.Sprintf("%s key: %s secret: %s", mp.name, mp.key, mp.secret)
}

func (mp *mockPublisher) Do(req shared.PublishRequest) error {

	r := rand.Intn(10)
	time.Sleep(time.Second * time.Duration(r))

	if r%2 == 0 {
		return errors.New("mock publish failed")
	}

	return nil
}
