package shared

import (
	"errors"
	"math/rand"
	"time"
)

type PublishRequest struct {
	AppName     string
	PackageName string
	VersionCode int32
	VersionName string

	ApkFile       string
	SecondApkFile string
	UpdateDesc    string

	// synchroType 更新类型：0=新增，1=更新包，2=内容更新
	SynchroType int
}

type Publisher interface {
	Do(req PublishRequest) error
	Name() string
}

type MockPublisher struct {
	real Publisher
}

func NewMockPublisher(r Publisher) *MockPublisher {
	return &MockPublisher{
		real: r,
	}
}

func (mp *MockPublisher) Name() string {
	return "mock-" + mp.real.Name()
}

func (mp *MockPublisher) Do(req PublishRequest) error {

	r := rand.Intn(10)
	time.Sleep(time.Second * time.Duration(10+r))

	if r%2 == 0 {
		return errors.New("mock publish failed")
	}

	return nil
}
