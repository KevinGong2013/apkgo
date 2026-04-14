package script

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/KevinGong2013/apkgo/pkg/progress"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("script", store.ConfigSchema{
		Name: "script",
		Fields: []store.FieldSchema{
			{Key: "command", Required: true, Desc: "Shell command or script path to execute"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

type Store struct {
	name    string
	command string
}

func New(cfg map[string]string) (*Store, error) {
	command := cfg["command"]
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}

	name := cfg["_name"]
	if name == "" {
		name = "script"
	}

	return &Store{name: name, command: command}, nil
}

func (s *Store) Name() string { return s.name }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()

	// Script stores don't stream bytes, but we still want the progress bar
	// to show a meaningful label ("running") instead of the default
	// "pending". Bytes stay at zero so the bar remains in spinner mode
	// until the uploader finalizes it with markDone.
	progress.Safe(req.Progress).Phase("running")

	input, err := json.Marshal(req)
	if err != nil {
		return store.ErrResult(s.Name(), start, fmt.Errorf("marshal input: %w", err))
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", s.command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", s.command)
	}

	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		return store.ErrResult(s.Name(), start, fmt.Errorf("%s", msg))
	}

	return store.NewResult(s.Name(), start)
}
