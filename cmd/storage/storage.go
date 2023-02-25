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
}

func New(c Config) (*Storage, error) {
	switch c.Location {
	case "local":
		return &Storage{
			isLocal: true,
		}, nil
	case "git":
		var auth transport.AuthMethod
		fmt.Println(c.Key)
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
			gitCloneOptions: git.CloneOptions{
				URL:             c.URL,
				Auth:            auth,
				ReferenceName:   "refs/heads/master",
				SingleBranch:    true,
				Depth:           1,
				InsecureSkipTLS: true,
				Progress:        os.Stdout,
			},
		}, nil
	default:
		return nil, fmt.Errorf("invalid location. %s", c)
	}
}

func (s *Storage) Mkdir(path string) error {
	if s.isLocal {
		return os.MkdirAll(path, 0755)
	}

	// 没有git仓库需要去clone
	_, err := git.PlainClone(path, false, &s.gitCloneOptions)
	if err != nil {
		return err
	}

	return nil
}
