package shared

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
}
