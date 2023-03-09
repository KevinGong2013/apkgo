package storage

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

type Config struct {
	Location string `yaml:"location"`
	URL      string `yaml:"url,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Key      string `yaml:"private_key,omitempty"`
}

type Storage struct {
	isLocal         bool
	gitCloneOptions git.CloneOptions
	path            string
}

func New(c Config, path string) (*Storage, error) {
	switch c.Location {
	case "local":
		return &Storage{
			isLocal: true,
			path:    path,
		}, nil
	case "git":
		var auth transport.AuthMethod
		if len(c.Key) > 0 {
			publicKey, err := ssh.NewPublicKeysFromFile("git", c.Key, c.Password)
			if err != nil {
				return nil, err
			}
			auth = publicKey
		} else {
			auth = &http.BasicAuth{
				Username: c.Username,
				Password: c.Password,
			}
		}
		return &Storage{
			isLocal: false,
			path:    path,
			gitCloneOptions: git.CloneOptions{
				URL:             c.URL,
				Auth:            auth,
				ReferenceName:   "refs/heads/master",
				SingleBranch:    true,
				InsecureSkipTLS: true,
				Progress:        os.Stdout,
			},
		}, nil
	default:
		return nil, fmt.Errorf("invalid location. %s", c)
	}
}

// 第一次
func (s *Storage) EnsureDir() error {

	if s.isLocal {
		return os.MkdirAll(s.path, 0755)
	}

	// 没有git仓库需要去clone
	os.RemoveAll(s.path)
	_, err := git.PlainClone(s.path, false, &s.gitCloneOptions)
	if err != nil {
		return err
	}

	return nil
}

func (s *Storage) UpToDate() error {
	if s.isLocal {
		return nil
	}

	if err := s.upToDateIfLocalClean(); err != nil {
		return err
	}

	return nil
}

// 推送本地的授权信息
func (s *Storage) Sync() error {
	if s.isLocal {
		return nil
	}

	return s.commitAndPushLocalChanges()
}
