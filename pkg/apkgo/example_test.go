package apkgo_test

import (
	"context"
	"fmt"

	"github.com/KevinGong2013/apkgo/pkg/apkgo"
	"github.com/KevinGong2013/apkgo/pkg/config"
)

// ExampleRun shows the minimal embedding of apkgo into another Go
// program (cloud workers, custom CI, etc.). Construct a config in code
// or load from disk, fill out a Job, call Run, inspect the Result.
func ExampleRun() {
	cfg := &config.Config{
		Stores: map[string]map[string]string{
			"huawei": {
				"service_account_file": "/secure/huawei-sa.json",
			},
			"tencent": {
				"user_id":       "...",
				"access_secret": "...",
				"app_id_map":    `{"com.example.foo":"111","com.example.bar":"222"}`,
			},
		},
	}

	result, err := apkgo.Run(context.Background(), apkgo.Job{
		APKFile: "https://artifacts.example.com/foo-v1.apk",
		Stores:  []string{"huawei", "tencent"},
		Notes:   "Bug fixes",
		Config:  cfg,
	})
	if err != nil {
		fmt.Printf("run failed: %v\n", err)
		return
	}
	for _, r := range result.Results {
		if r.Success {
			fmt.Printf("%s: ok in %dms\n", r.Store, r.DurationMs)
		} else {
			fmt.Printf("%s: %s\n", r.Store, r.Error)
		}
	}
}
