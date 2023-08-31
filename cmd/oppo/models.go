package oppo

import (
	"encoding/json"
	"net/url"
	"strconv"
)

type app struct {
	AppID                     string             `json:"app_id"`
	PkgName                   string             `json:"pkg_name"`
	Type                      int                `json:"type"`
	Sign                      string             `json:"sign"`
	DevID                     string             `json:"dev_id"`
	AppSecret                 string             `json:"app_secret"`
	ServerSecret              string             `json:"server_secret"`
	AppKey                    string             `json:"app_key"`
	UpdateTime                string             `json:"update_time"`
	AppCreateTime             string             `json:"app_create_time"`
	AppName                   string             `json:"app_name"`
	IsFreeze                  string             `json:"is_freeze"`
	FreezeReason              string             `json:"freeze_reason"`
	RefuseReason              string             `json:"refuse_reason"`
	TagList                   string             `json:"tag_list"`
	IsBusiness                string             `json:"is_business"`
	GameType                  string             `json:"game_type"`
	SecondCategoryID          string             `json:"second_category_id"`
	ThirdCategoryID           string             `json:"third_category_id"`
	CopyrightURL              string             `json:"copyright_url"`
	SpecialURL                string             `json:"special_url"`
	SpecialFileURL            string             `json:"special_file_url"`
	FreezeFile                string             `json:"freeze_file"`
	BusinessUsername          string             `json:"business_username"`
	BusinessEmail             string             `json:"business_email"`
	BusinessMobile            string             `json:"business_mobile"`
	BusinessQQ                string             `json:"business_qq"`
	BusinessPosition          string             `json:"business_position"`
	BusinessAddress           string             `json:"business_address"`
	FreezeAdvice              string             `json:"freeze_advice"`
	AppType                   string             `json:"app_type"`
	AppRealType               string             `json:"app_real_type"`
	AdType                    string             `json:"ad_type"`
	DevName                   string             `json:"dev_name"`
	ElectronicCertURL         string             `json:"electronic_cert_url"`
	ICPURL                    string             `json:"icp_url"`
	RelationAppID             string             `json:"relation_app_id"`
	AscriptionType            string             `json:"ascription_type"`
	AuthorizeType             string             `json:"authorize_type"`
	ProxyContractURL          string             `json:"proxy_contract_url"`
	AuthorizeURL              string             `json:"authorize_url"`
	AuthorizeDesc             string             `json:"authorize_desc"`
	OperationLicenseURL       string             `json:"operation_license_url"`
	ApprovalDocURL            string             `json:"approval_doc_url"`
	CultureRecordURL          string             `json:"culture_record_url"`
	ApprovalDocNumber         string             `json:"approval_doc_number"`
	CultureRecordNumber       string             `json:"culture_record_number"`
	ApprovalDocType           string             `json:"approval_doc_type"`
	ApprovalDocStartTime      string             `json:"approval_doc_start_time"`
	ApprovalDocEndTime        string             `json:"approval_doc_end_time"`
	OtherCertificateURL       string             `json:"other_cetificate_url"`
	AbsolveDeclareURL         string             `json:"absolve_declare_url"`
	RecordIdentificationCode  string             `json:"record_identification_code"`
	RecordIdentificationImage string             `json:"record_identification_image"`
	VersionID                 string             `json:"version_id"`
	VersionCode               string             `json:"version_code"`
	VersionName               string             `json:"version_name"`
	ApkURL                    string             `json:"apk_url"`
	ApkSize                   string             `json:"apk_size"`
	ApkMD5                    string             `json:"apk_md5"`
	HeaderMD5                 string             `json:"header_md5"`
	Channel                   string             `json:"channel"`
	PackagePermission         string             `json:"package_permission"`
	Resolution                string             `json:"resolution"`
	VersionType               string             `json:"version_type"`
	CreateTime                string             `json:"create_time"`
	MinSDKVersion             string             `json:"min_sdk_version"`
	TargetSDKVersion          string             `json:"target_sdk_version"`
	VerSecondCategoryID       string             `json:"ver_second_category_id"`
	VerThirdCategoryID        string             `json:"ver_third_category_id"`
	ReleaseType               string             `json:"release_type"`
	ReleaseOverType           string             `json:"release_over_type"`
	PhoneSupport              string             `json:"phone_support"`
	PhoneSupportVersion       string             `json:"phone_support_version"`
	IOSLink                   string             `json:"ios_link"`
	ApkFullURL                string             `json:"apk_full_url"`
	OnlineType                string             `json:"online_type"`
	ScheOnlineTime            string             `json:"sche_online_time"`
	TestType                  string             `json:"test_type"`
	TestStartTime             string             `json:"test_start_time"`
	TestEndTime               string             `json:"test_end_time"`
	IsSignature               string             `json:"is_signature"`
	IsPreDownload             string             `json:"is_pre_download"`
	Lang                      string             `json:"lang"`
	IconURL                   string             `json:"icon_url"`
	IconMD5                   string             `json:"icon_md5"`
	Summary                   string             `json:"summary"`
	DetailDesc                string             `json:"detail_desc"`
	UpdateDesc                string             `json:"update_desc"`
	AppSubname                string             `json:"app_subname"`
	TestDesc                  string             `json:"test_desc"`
	VideoURL                  string             `json:"video_url"`
	PicURL                    string             `json:"pic_url"`
	PackagePermissionDesc     string             `json:"package_permission_desc"`
	VideoPicURL               string             `json:"video_pic_url"`
	CoverURL                  string             `json:"cover_url"`
	LandscapePicURL           string             `json:"landscape_pic_url"`
	PrivacySourceURL          string             `json:"privacy_source_url"`
	ReleaseDesc               string             `json:"release_desc"`
	TestURL                   string             `json:"test_url"`
	EnglishName               string             `json:"english_name"`
	Region                    string             `json:"region"`
	Level                     string             `json:"level"`
	OnlineTime                string             `json:"online_time"`
	OfflineTime               string             `json:"offline_time"`
	IsFirstPublish            string             `json:"is_first_publish"`
	BusinessRefuseReason      string             `json:"business_refuse_reason"`
	OldAuditStatus            string             `json:"old_audit_status"`
	ReleaseStatus             string             `json:"release_status"`
	RefuseAdvice              string             `json:"refuse_advice"`
	State                     string             `json:"state"`
	ChangeState               string             `json:"change_state"`
	OnlineInfoOfflineApply    []offlineApplyInfo `json:"online_info_offline_apply"`
	Size                      string             `json:"size"`
	AuditStatus               string             `json:"audit_status"`
	AuditStatusName           string             `json:"audit_status_name"`
	OfflineInfo               string             `json:"offline_info"`
	TransferState             int                `json:"transfer_state"`
	UpdateInfoCheck           int                `json:"update_info_check"`
	LevelTag                  string             `json:"level_tag"`
	RefuseFile                string             `json:"refuse_file"`
	LandscapePicURLMaterial   []picMaterialInfo  `json:"landscape_pic_url_material"`
	PicURLMaterial            []picMaterialInfo  `json:"pic_url_material"`
	VideoURLMaterial          []videoInfo        `json:"video_url_material"`
	AgeLevel                  string             `json:"age_level"`
	AdaptiveEquipment         string             `json:"adaptive_equipment"`
	AdaptiveType              string             `json:"adaptive_type"`
	CustomerContact           string             `json:"customer_contact"`
}

type offlineApplyInfo struct {
	OfflineApplyID string `json:"offline_apply_id"`
	AppID          string `json:"app_id"`
	ApplyType      string `json:"apply_type"`
	ApplyTime      string `json:"apply_time"`
	ApplyReason    string `json:"apply_reason"`
	ApplyStatus    string `json:"apply_status"`
	AuditTime      string `json:"audit_time"`
	AuditReason    string `json:"audit_reason"`
	AuditUser      string `json:"audit_user"`
}

type picMaterialInfo struct {
	PicURL      string `json:"pic_url"`
	PicMD5      string `json:"pic_md5"`
	PicWidth    int    `json:"pic_width"`
	PicHeight   int    `json:"pic_height"`
	PicFileSize int    `json:"pic_file_size"`
}

type apkInfo struct {
	Url     string `json:"url"`
	Md5     string `json:"md5"`
	CpuCode int    `json:"cpu_code"`
}

type coverURLInfo struct {
	CoverUrl string `json:"cover_url"`
	CoverMd5 string `json:"cover_md5"`
}

type videoInfo struct {
	VideoUrl string `json:"video_url"`
	VideoMd5 string `json:"video_md5"`
}

type publishRequestParameter struct {
	PkgName                   string       `json:"pkg_name"`
	VersionCode               string       `json:"version_code"`
	ApkUrl                    []apkInfo    `json:"apk_url"`
	AppName                   string       `json:"app_name"`
	AppSubname                string       `json:"app_subname,omitempty"`
	SecondCategoryId          int          `json:"second_category_id"`
	ThirdCategoryId           int          `json:"third_category_id"`
	Summary                   string       `json:"summary"`
	DetailDesc                string       `json:"detail_desc"`
	UpdateDesc                string       `json:"update_desc"`
	PrivacySourceUrl          string       `json:"privacy_source_url"`
	IconUrl                   string       `json:"icon_url"`
	PicUrl                    string       `json:"pic_url"`
	LandscapePicUrl           string       `json:"landscape_pic_url,omitempty"`
	VideoUrl                  string       `json:"video_url,omitempty"`
	VideoUrlMaterial          []videoInfo  `json:"video_url_material,omitempty"`
	OnlineType                int          `json:"online_type"`
	ScheOnlineTime            string       `json:"sche_online_time,omitempty"`
	TestDesc                  string       `json:"test_desc"`
	ElectronicCertUrl         string       `json:"electronic_cert_url,omitempty"`
	CopyrightUrl              string       `json:"copyright_url"`
	IcpUrl                    string       `json:"icp_url,omitempty"`
	SpecialUrl                string       `json:"special_url,omitempty"`
	SpecialFileUrl            string       `json:"special_file_url,omitempty"`
	GameType                  int          `json:"game_type,omitempty"`
	VideoPicUrl               string       `json:"video_pic_url,omitempty"`
	CoverUrl                  coverURLInfo `json:"cover_url"`
	AscriptionType            int          `json:"ascription_type,omitempty"`
	ProxyContractUrl          string       `json:"proxy_contract_url,omitempty"`
	AuthorizeType             int          `json:"authorize_type,omitempty"`
	AuthorizeUrl              string       `json:"authorize_url,omitempty"`
	AuthorizeDesc             string       `json:"authorize_desc,omitempty"`
	ApprovalDocNumber         string       `json:"approval_doc_number,omitempty"`
	ApprovalDocType           int          `json:"approval_doc_type,omitempty"`
	ApprovalDocStartTime      string       `json:"approval_doc_start_time,omitempty"`
	ApprovalDocEndTime        string       `json:"approval_doc_end_time,omitempty"`
	ApprovalDocUrl            string       `json:"approval_doc_url,omitempty"`
	RecordIdentificationCode  string       `json:"record_identification_code,omitempty"`
	RecordIdentificationImage string       `json:"record_identification_image,omitempty"`
	CultureRecordNumber       string       `json:"culture_record_number,omitempty"`
	CultureRecordUrl          string       `json:"culture_record_url,omitempty"`
	OperationLicenseUrl       string       `json:"operation_license_url,omitempty"`
	AbsolveDeclareUrl         string       `json:"absolve_declare_url,omitempty"`
	BusinessUsername          string       `json:"business_username"`
	BusinessEmail             string       `json:"business_email"`
	BusinessMobile            string       `json:"business_mobile"`
	BusinessQQ                string       `json:"business_qq,omitempty"`
	BusinessPosition          string       `json:"business_position,omitempty"`
	BusinessAddress           string       `json:"business_address,omitempty"`
	AgeLevel                  int          `json:"age_level"`
	AdaptiveEquipment         string       `json:"adaptive_equipment"`
	AdaptiveType              string       `json:"adaptive_type,omitempty"`
	CustomerContact           string       `json:"customer_contact,omitempty"`
}

func (pp publishRequestParameter) toValues() url.Values {
	// convert to url.Values
	values := url.Values{}

	values.Set("pkg_name", pp.PkgName)
	values.Set("version_code", pp.VersionCode)
	values.Set("app_name", pp.AppName)
	values.Set("second_category_id", strconv.Itoa(pp.SecondCategoryId))
	values.Set("third_category_id", strconv.Itoa(pp.ThirdCategoryId))
	values.Set("summary", pp.Summary)
	values.Set("detail_desc", pp.DetailDesc)
	values.Set("update_desc", pp.UpdateDesc)
	values.Set("privacy_source_url", pp.PrivacySourceUrl)
	values.Set("icon_url", pp.IconUrl)
	values.Set("pic_url", pp.PicUrl)
	values.Set("video_url", pp.VideoUrl)
	values.Set("online_type", strconv.Itoa(pp.OnlineType))
	values.Set("sche_online_time", pp.ScheOnlineTime)
	values.Set("test_desc", pp.TestDesc)
	values.Set("electronic_cert_url", pp.ElectronicCertUrl)
	values.Set("icp_url", pp.IcpUrl)
	values.Set("special_url", pp.SpecialUrl)
	values.Set("special_file_url", pp.SpecialFileUrl)
	values.Set("game_type", strconv.Itoa(pp.GameType))

	values.Set("video_pic_url", pp.VideoPicUrl)
	values.Set("cover_url", pp.CoverUrl.CoverUrl)
	values.Set("ascription_type", strconv.Itoa(pp.AscriptionType))
	values.Set("proxy_contract_url", pp.ProxyContractUrl)
	values.Set("authorize_type", strconv.Itoa(pp.AuthorizeType))
	values.Set("authorize_desc", pp.AuthorizeDesc)
	values.Set("approval_doc_number", pp.ApprovalDocNumber)
	values.Set("approval_doc_type", strconv.Itoa(pp.ApprovalDocType))
	values.Set("approval_doc_start_time", pp.ApprovalDocStartTime)
	values.Set("approval_doc_end_time", pp.ApprovalDocEndTime)
	values.Set("approval_doc_url", pp.ApprovalDocUrl)
	values.Set("record_identification_code", pp.RecordIdentificationCode)
	values.Set("record_identification_image", pp.RecordIdentificationImage)
	values.Set("culture_record_number", pp.CultureRecordNumber)
	values.Set("culture_record_url", pp.CultureRecordUrl)
	values.Set("operation_license_url", pp.OperationLicenseUrl)
	values.Set("absolve_declare_url", pp.AbsolveDeclareUrl)
	values.Set("business_username", pp.BusinessUsername)
	values.Set("business_email", pp.BusinessEmail)
	values.Set("business_mobile", pp.BusinessMobile)
	values.Set("business_qq", pp.BusinessQQ)
	values.Set("business_position", pp.BusinessPosition)
	values.Set("business_address", pp.BusinessAddress)
	values.Set("age_level", strconv.Itoa(pp.AgeLevel))
	values.Set("adaptive_equipment", pp.AdaptiveEquipment)
	values.Set("adaptive_type", "2")
	values.Set("customer_contact", pp.CustomerContact)

	values.Set("copyright_url", pp.CopyrightUrl)

	values.Set("authorize_url", pp.AuthorizeUrl)

	bytes, _ := json.Marshal(pp.ApkUrl)
	values.Set("apk_url", string(bytes))

	return values
}

type uploadResult struct {
	URL           string `json:"url"`
	URIPath       string `json:"uri_path"`
	MD5           string `json:"md5"`
	FileExtension string `json:"file_extension"`
	FileSize      int    `json:"file_size"`
	ID            string `json:"id"`
}

type taskBody struct {
	PkgName     string `json:"pkg_name"`
	VersionCode string `json:"version_code"`
	TaskState   string `json:"task_state"` // 状态，1-待处理；2-处理成功；3-处理失败
	ErrMsg      string `json:"err_msg"`
}
