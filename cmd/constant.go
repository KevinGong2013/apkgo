package cmd

import "path/filepath"

const ConfigFileName = "config.yaml"
const StoreConfigFileName = "store.json"

func storeCfgFilePath() string {
	return filepath.Join(apkgoHome, StoreConfigFileName)
}
