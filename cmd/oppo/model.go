package oppo

type verifyInfoResponse struct {
	Errno int `json:"errno"`
	Data  struct {
		Message           string   `json:"message"`
		AppName           string   `json:"app_name"`
		ApkMd5            string   `json:"apk_md5"`
		MinSdkVersion     int      `json:"min_sdk_version"`
		PackagePermission []string `json:"package_permission"`
		HeaderMd5         string   `json:"header_md5"`
		PkgName           string   `json:"pkg_name"`
		TargetSdkVersion  int      `json:"target_sdk_version"`
		VersionName       string   `json:"version_name"`
		VersionCode       int      `json:"version_code"`
		Sign              string   `json:"sign"`
		ApkSize           int      `json:"apk_size"`
		AllSignList       struct {
			V1 []string `json:"V1"`
			V2 []string `json:"V2"`
		} `json:"all_sign_list"`
		IsSplitApk bool `json:"is_split_apk"`
		Sha1List   []struct {
			V1 string `json:"V1,omitempty"`
			V2 string `json:"V2,omitempty"`
		} `json:"sha1_list"`
		Sha256List []struct {
			V1 string `json:"V1,omitempty"`
			V2 string `json:"V2,omitempty"`
		} `json:"sha256_list"`
		HasLauncher           bool     `json:"has_launcher"`
		CertBase64            string   `json:"cert_base64"`
		PackagePermissionDesc []string `json:"package_permission_desc"`
		NativeCode            string   `json:"native_code"`
		GameMode              string   `json:"game_mode"`
		PaySdkKey             string   `json:"pay_sdk_key"`
		SignV1                string   `json:"sign_v1"`
		SignV2                string   `json:"sign_v2"`
		SignV3                string   `json:"sign_v3"`
		Sha1V1                string   `json:"sha1_v1"`
		Sha1V2                string   `json:"sha1_v2"`
		Sha1V3                string   `json:"sha1_v3"`
		Sha256V1              string   `json:"sha256_v1"`
		Sha256V2              string   `json:"sha256_v2"`
		Sha256V3              string   `json:"sha256_v3"`
		LocalOcsApkURL        string   `json:"local_ocs_apk_url"`
		OriginalSign          string   `json:"original_sign"`
		CheckSkdResult        struct {
			AllianceSdk    []interface{} `json:"alliance_sdk"`
			CooperationSdk []interface{} `json:"cooperation_sdk"`
		} `json:"check_skd_result"`
	} `json:"data"`
}
