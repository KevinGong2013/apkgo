package cmd

import "path/filepath"

const ConfigFileName = "config.yaml"
const StoreConfigFileName = "store_config.json"
const SecretDirName = "secrets/"

func storeCfgFilePath() string {
	return filepath.Join(apkgoHome, SecretDirName, StoreConfigFileName)
}

func configFilePath() string {
	return filepath.Join(apkgoHome, ConfigFileName)
}

func browserUserDataDir() string {
	return filepath.Join(apkgoHome, SecretDirName, "chrome_user_data")
}
