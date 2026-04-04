package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/KevinGong2013/apkgo/pkg/apk"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

// Payload types for hook stdin JSON.

type BeforeAllPayload struct {
	FilePath string   `json:"file_path"`
	APK      *apk.Info `json:"apk"`
	Stores   []string  `json:"stores"`
}

type AfterAllPayload struct {
	FilePath string                `json:"file_path"`
	APK      *apk.Info             `json:"apk"`
	Results  []*store.UploadResult `json:"results"`
}

type BeforeStorePayload struct {
	FilePath string    `json:"file_path"`
	APK      *apk.Info `json:"apk"`
	Store    string    `json:"store"`
}

type AfterStorePayload struct {
	FilePath string              `json:"file_path"`
	APK      *apk.Info           `json:"apk"`
	Store    string              `json:"store"`
	Result   *store.UploadResult `json:"result"`
}

// RunHook executes a shell command, piping payload as JSON to stdin.
// Returns nil on success, an error if the command fails (non-zero exit).
func RunHook(ctx context.Context, command string, payload any, envVars map[string]string) error {
	input, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal hook payload: %w", err)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Set environment variables for simple scripts
	for k, v := range envVars {
		cmd.Env = append(cmd.Environ(), fmt.Sprintf("%s=%s", k, v))
	}

	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
