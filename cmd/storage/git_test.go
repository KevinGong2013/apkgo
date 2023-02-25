package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestDownload(t *testing.T) {
	// 这里的user 只能是`git`，奇怪的参数
	// privateKey, err1 := ssh.NewPublicKeysFromFile("git", "/Users/gix/.ssh/id_rsa", "")
	// if err1 != nil {
	// 	fmt.Println(err1)
	// }

	// Auth: &http.BasicAuth{
	// 		Username: "kevin",
	// 		Password: "",
	// 	},

	secretsDir := filepath.Join("/Users/gix/.apkgo", "secrets")

	var repo *git.Repository
	var err error
	repo, err = git.PlainOpen(secretsDir)
	if err != nil {
		// 没有git仓库需要去clone
		fmt.Println(err)
		repo, err = git.PlainClone(secretsDir, false, &git.CloneOptions{
			URL: "http://git.yuxiaor.com/yuxiaor-mobile/apkgo-conf.git",
			// URL:             "git@git.yuxiaor.com:yuxiaor-mobile/apkgo-conf.git",
			ReferenceName:   "refs/heads/master",
			Auth:            &http.BasicAuth{Username: "kevin", Password: "aoxianglele"},
			SingleBranch:    true,
			Depth:           1,
			InsecureSkipTLS: true,
			Progress:        os.Stdout,
		})
		if err != nil {
			panic(err)
		}
	}

	err = commitAndPushLocalChanges(repo)
	// err = ensureRepoCleanAndUpToDate(repo)

	if err != nil {
		fmt.Println(err)
	}
}

// storage
// local -> 单机使用
// git -> 团队多个人使用 / 配合CI/CD使用

// 所以需要 init 命令

// 使用场景， 任何人都需要先 apk init
// 判断没有config 文件就提醒执行 apk init
//
// config.yaml
// secrets
// 	 / store.json
//   / chrome
//   / README.md

// 1. 选择 storage
// 2. git url/ u/p user/token private/key
// 3. 直接尝试从上面的git拉取信息

// 4. 可以自己开始配置
// 你

// 5. 是否用自己本地鉴权信息 覆盖git仓库的版本
// 6. 如果是的话这边开始提交
