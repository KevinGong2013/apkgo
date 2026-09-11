package apk

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// Platform identifiers reported in Info.Platform.
const (
	PlatformAndroid = "android"
	PlatformHarmony = "harmony"
)

// IsHarmony reports whether path looks like a HarmonyOS package, based on
// the file extension (case-insensitive): `.app` is an App Pack (the unit
// AppGallery Connect accepts for release, bundling one or more HAPs) and
// `.hap` is a single HarmonyOS Ability Package module.
func IsHarmony(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".app", ".hap":
		return true
	}
	return false
}

// IsHarmonyAppPack reports whether path is a HarmonyOS App Pack (`.app`),
// the only package form AGC's Publishing API accepts for a release.
func IsHarmonyAppPack(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".app")
}

// packInfo mirrors the parts of pack.info apkgo needs. Both `.app` packs
// and individual `.hap` modules carry it at the archive root; it is plain
// JSON written by the packing tool, so it is the most stable source for
// bundleName / versionCode / versionName across stage-model and legacy
// FA-model packages.
type packInfo struct {
	Summary struct {
		App struct {
			BundleName string `json:"bundleName"`
			BundleType string `json:"bundleType"`
			Version    struct {
				Code json.Number `json:"code"`
				Name string      `json:"name"`
			} `json:"version"`
		} `json:"app"`
		Modules []struct {
			Distro struct {
				ModuleType string `json:"moduleType"`
				ModuleName string `json:"moduleName"`
			} `json:"distro"`
		} `json:"modules"`
	} `json:"summary"`
	Packages []struct {
		Name       string `json:"name"`
		ModuleType string `json:"moduleType"`
	} `json:"packages"`
}

// ParseHarmony extracts metadata from a HarmonyOS `.app` pack or `.hap`
// module. bundleName / versionCode / versionName come from pack.info;
// AppName is resolved best-effort from the entry module's module.json
// `app.label` reference via resources.index and is left empty when it
// cannot be resolved (it is informational only — AGC takes the listing
// name from the console, not from the package).
func ParseHarmony(path string) (*Info, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open harmony package: %w", err)
	}
	defer zr.Close()

	pi, err := readPackInfo(&zr.Reader)
	if err != nil {
		return nil, err
	}
	if pi.Summary.App.BundleName == "" {
		return nil, fmt.Errorf("parse harmony package: pack.info has no summary.app.bundleName")
	}
	code, err := strconv.ParseInt(pi.Summary.App.Version.Code.String(), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("parse harmony package: invalid summary.app.version.code: %w", err)
	}

	info := &Info{
		Platform:    PlatformHarmony,
		PackageName: pi.Summary.App.BundleName,
		VersionName: pi.Summary.App.Version.Name,
		VersionCode: int32(code),
	}

	// Best-effort label. A .hap is itself the module; a .app pack nests the
	// modules as <package name>.hap entries.
	if strings.EqualFold(filepath.Ext(path), ".hap") {
		info.AppName = labelFromModule(&zr.Reader)
	} else if entry := findEntryHap(&zr.Reader, pi); entry != nil {
		if inner := openNestedZip(entry); inner != nil {
			info.AppName = labelFromModule(inner)
		}
	}
	return info, nil
}

func readPackInfo(zr *zip.Reader) (*packInfo, error) {
	f := findEntry(zr, "pack.info")
	if f == nil {
		return nil, fmt.Errorf("parse harmony package: pack.info not found (not a HarmonyOS .app/.hap?)")
	}
	data, err := readEntry(f, 4<<20)
	if err != nil {
		return nil, fmt.Errorf("parse harmony package: read pack.info: %w", err)
	}
	var pi packInfo
	if err := json.Unmarshal(data, &pi); err != nil {
		return nil, fmt.Errorf("parse harmony package: decode pack.info: %w", err)
	}
	return &pi, nil
}

// findEntryHap locates the entry module's HAP inside an .app pack: the
// `packages[]` item with moduleType "entry" names it (`<name>.hap`);
// failing that, any .hap entry is used.
func findEntryHap(zr *zip.Reader, pi *packInfo) *zip.File {
	for _, p := range pi.Packages {
		if p.ModuleType == "entry" && p.Name != "" {
			if f := findEntry(zr, p.Name+".hap"); f != nil {
				return f
			}
		}
	}
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Ext(f.Name), ".hap") {
			return f
		}
	}
	return nil
}

// openNestedZip returns a zip.Reader over a HAP stored inside an .app
// pack. The module bytes are buffered in memory up to a size cap; larger
// modules just skip label resolution, which is informational only.
func openNestedZip(f *zip.File) *zip.Reader {
	const maxBuffer = 256 << 20
	if f.UncompressedSize64 > maxBuffer {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxBuffer))
	if err != nil {
		return nil
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	return zr
}

// moduleJSON mirrors the compiled stage-model module.json fields used
// to resolve the app label. Legacy FA-model packages carry config.json
// with the same `app` block layout for our purposes.
type moduleJSON struct {
	App struct {
		Label string `json:"label"`
	} `json:"app"`
}

// labelFromModule resolves the human-readable app name of a HAP:
// module.json → app.label ("$string:app_name" or "$string:16777216")
// → resources.index STRING entry in the base (locale-less) qualifier.
// Returns "" when any step fails.
func labelFromModule(zr *zip.Reader) string {
	mf := findEntry(zr, "module.json")
	if mf == nil {
		mf = findEntry(zr, "config.json")
	}
	if mf == nil {
		return ""
	}
	data, err := readEntry(mf, 4<<20)
	if err != nil {
		return ""
	}
	var mj moduleJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		return ""
	}
	ref := strings.TrimSpace(mj.App.Label)
	if !strings.HasPrefix(ref, "$string:") {
		// A literal label (rare, but allowed for FA-model config.json).
		if ref != "" && !strings.HasPrefix(ref, "$") {
			return ref
		}
		return ""
	}
	key := strings.TrimPrefix(ref, "$string:")
	var wantID uint32
	wantName := key
	if n, err := strconv.ParseUint(key, 10, 32); err == nil {
		wantID = uint32(n)
		wantName = ""
	}

	rf := findEntry(zr, "resources.index")
	if rf == nil {
		return ""
	}
	idx, err := readEntry(rf, 64<<20)
	if err != nil {
		return ""
	}
	val, ok := lookupIndexString(idx, wantName, wantID)
	if !ok {
		return ""
	}
	return val
}

func findEntry(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func readEntry(f *zip.File, limit int64) ([]byte, error) {
	if int64(f.UncompressedSize64) > limit {
		return nil, fmt.Errorf("%s: %d bytes exceeds limit %d", f.Name, f.UncompressedSize64, limit)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit+1))
}

// ---------------------------------------------------------------------
// resources.index reader
//
// The binary layout follows OpenHarmony's global_resource_management
// (frameworks/resmgr/include/res_desc.h). Two generations exist:
//
//   v1 ("Restool ..." version string, RES_HEADER_LEN 136):
//     header  : version[128] | u32 length | u32 keyCount
//     keys    : "KEYS" | u32 idsOffset | u32 paramCount | (u32 type, u32 value)*
//     ids     : "IDSS" | u32 count | (u32 id, u32 itemOffset)*
//     item    : u32 size | u32 resType | u32 id | u16 vLen | value(vLen, NUL-terminated)
//               | u16 nLen | name(nLen, NUL-terminated)
//
//   v2 (any other version string, RES_HEADER_LEN 140):
//     header  : version[128] | u32 length | u32 keyCount | u32 dataOffset
//     keys    : "KEYS" | u32 cfgId | u32 paramCount | (u32 type, u32 value)*
//     ids     : "IDSS" | u32 len | u32 typeCount | u32 idCount
//               typeCount × { u32 type | u32 len | u32 count
//                             count × { u32 id | u32 infoOffset | u32 nLen | name } }
//     info    : u32 id | u32 len | u32 valueCount | valueCount × (u32 cfgId, u32 valOffset)
//     value   : u16 len | bytes
//
// Only STRING (resType 9) entries are consulted, preferring the base
// qualifier (no language/region/script key params) so a localized name
// never shadows the default one.
// ---------------------------------------------------------------------

const (
	resVersionLen = 128
	resTypeString = 9

	keyTypeLanguages = 0
	keyTypeRegion    = 1
	keyTypeScript    = 5
)

type idxReader struct{ b []byte }

func (r idxReader) u16(off int) (uint16, bool) {
	if off < 0 || off+2 > len(r.b) {
		return 0, false
	}
	return binary.LittleEndian.Uint16(r.b[off:]), true
}

func (r idxReader) u32(off int) (uint32, bool) {
	if off < 0 || off+4 > len(r.b) {
		return 0, false
	}
	return binary.LittleEndian.Uint32(r.b[off:]), true
}

func (r idxReader) tag(off int, want string) bool {
	return off >= 0 && off+4 <= len(r.b) && string(r.b[off:off+4]) == want
}

// isBaseQualifier reports whether a key's params carry no locale
// qualifier. off points at the first (type,value) pair.
func (r idxReader) isBaseQualifier(off int, count uint32) (bool, int) {
	base := true
	for i := uint32(0); i < count; i++ {
		t, ok := r.u32(off)
		if !ok {
			return false, off
		}
		switch t {
		case keyTypeLanguages, keyTypeRegion, keyTypeScript:
			base = false
		}
		off += 8
	}
	return base, off
}

func lookupIndexString(idx []byte, wantName string, wantID uint32) (string, bool) {
	if len(idx) < resVersionLen+8 {
		return "", false
	}
	ver := idx[:resVersionLen]
	if i := bytes.IndexByte(ver, 0); i >= 0 {
		ver = ver[:i]
	}
	if strings.HasPrefix(string(ver), "Restool") {
		return lookupIndexStringV1(idxReader{idx}, wantName, wantID)
	}
	return lookupIndexStringV2(idxReader{idx}, wantName, wantID)
}

func lookupIndexStringV1(r idxReader, wantName string, wantID uint32) (string, bool) {
	keyCount, ok := r.u32(resVersionLen + 4)
	if !ok || keyCount == 0 || keyCount > 1<<20 {
		return "", false
	}
	type key struct {
		idsOffset uint32
		base      bool
	}
	keys := make([]key, 0, keyCount)
	off := resVersionLen + 8
	for i := uint32(0); i < keyCount; i++ {
		if !r.tag(off, "KEYS") {
			return "", false
		}
		idsOff, ok1 := r.u32(off + 4)
		pc, ok2 := r.u32(off + 8)
		if !ok1 || !ok2 || pc > 32 {
			return "", false
		}
		base, next := r.isBaseQualifier(off+12, pc)
		keys = append(keys, key{idsOffset: idsOff, base: base})
		off = next
	}
	// Base qualifier first, then anything else.
	for _, wantBase := range []bool{true, false} {
		for _, k := range keys {
			if k.base != wantBase {
				continue
			}
			if v, ok := r.v1ScanIDs(int(k.idsOffset), wantName, wantID); ok {
				return v, true
			}
		}
	}
	return "", false
}

func (r idxReader) v1ScanIDs(off int, wantName string, wantID uint32) (string, bool) {
	if !r.tag(off, "IDSS") {
		return "", false
	}
	count, ok := r.u32(off + 4)
	if !ok || count > 1<<24 {
		return "", false
	}
	off += 8
	for i := uint32(0); i < count; i++ {
		id, ok1 := r.u32(off)
		itemOff, ok2 := r.u32(off + 4)
		if !ok1 || !ok2 {
			return "", false
		}
		off += 8
		if wantID != 0 && id != wantID {
			continue
		}
		if v, ok := r.v1Item(int(itemOff), wantName, wantID); ok {
			return v, true
		}
	}
	return "", false
}

func (r idxReader) v1Item(off int, wantName string, wantID uint32) (string, bool) {
	resType, ok1 := r.u32(off + 4)
	id, ok2 := r.u32(off + 8)
	if !ok1 || !ok2 || resType != resTypeString {
		return "", false
	}
	if wantID != 0 && id != wantID {
		return "", false
	}
	off += 12
	vLen, ok := r.u16(off)
	if !ok || vLen == 0 || off+2+int(vLen) > len(r.b) {
		return "", false
	}
	value := string(r.b[off+2 : off+2+int(vLen)-1])
	off += 2 + int(vLen)
	nLen, ok := r.u16(off)
	if !ok || nLen == 0 || off+2+int(nLen) > len(r.b) {
		return "", false
	}
	name := string(r.b[off+2 : off+2+int(nLen)-1])
	if wantName != "" && name != wantName {
		return "", false
	}
	return value, true
}

func lookupIndexStringV2(r idxReader, wantName string, wantID uint32) (string, bool) {
	keyCount, ok1 := r.u32(resVersionLen + 4)
	dataOff, ok2 := r.u32(resVersionLen + 8)
	if !ok1 || !ok2 || keyCount == 0 || keyCount > 1<<20 {
		return "", false
	}
	_ = dataOff
	baseCfg := make(map[uint32]bool, keyCount)
	off := resVersionLen + 12
	for i := uint32(0); i < keyCount; i++ {
		if !r.tag(off, "KEYS") {
			return "", false
		}
		cfgID, ok1 := r.u32(off + 4)
		pc, ok2 := r.u32(off + 8)
		if !ok1 || !ok2 || pc > 32 {
			return "", false
		}
		base, next := r.isBaseQualifier(off+12, pc)
		baseCfg[cfgID] = base
		off = next
	}

	if !r.tag(off, "IDSS") {
		return "", false
	}
	typeCount, ok1 := r.u32(off + 8)
	if !ok1 || typeCount > 64 {
		return "", false
	}
	off += 16
	for t := uint32(0); t < typeCount; t++ {
		resType, ok1 := r.u32(off)
		blockLen, ok2 := r.u32(off + 4)
		count, ok3 := r.u32(off + 8)
		if !ok1 || !ok2 || !ok3 {
			return "", false
		}
		if resType != resTypeString {
			// TypeInfo.length_ covers the whole type block including its
			// own 12-byte header.
			if blockLen < 12 {
				return "", false
			}
			off += int(blockLen)
			continue
		}
		off += 12
		for i := uint32(0); i < count; i++ {
			id, ok1 := r.u32(off)
			infoOff, ok2 := r.u32(off + 4)
			nLen, ok3 := r.u32(off + 8)
			if !ok1 || !ok2 || !ok3 || nLen > 0xFFFF || off+12+int(nLen) > len(r.b) {
				return "", false
			}
			name := string(r.b[off+12 : off+12+int(nLen)])
			off += 12 + int(nLen)
			if wantID != 0 && id != wantID {
				continue
			}
			if wantName != "" && name != wantName {
				continue
			}
			if v, ok := r.v2Value(int(infoOff), id, baseCfg); ok {
				return v, true
			}
		}
		return "", false
	}
	return "", false
}

func (r idxReader) v2Value(off int, id uint32, baseCfg map[uint32]bool) (string, bool) {
	gotID, ok1 := r.u32(off)
	valueCount, ok2 := r.u32(off + 8)
	if !ok1 || !ok2 || gotID != id || valueCount == 0 || valueCount > 1<<20 {
		return "", false
	}
	off += 12
	var fallback int = -1
	for i := uint32(0); i < valueCount; i++ {
		cfgID, ok1 := r.u32(off)
		valOff, ok2 := r.u32(off + 4)
		if !ok1 || !ok2 {
			return "", false
		}
		off += 8
		if baseCfg[cfgID] {
			return r.v2String(int(valOff))
		}
		if fallback < 0 {
			fallback = int(valOff)
		}
	}
	if fallback >= 0 {
		return r.v2String(fallback)
	}
	return "", false
}

func (r idxReader) v2String(off int) (string, bool) {
	n, ok := r.u16(off)
	if !ok || off+2+int(n) > len(r.b) {
		return "", false
	}
	return string(r.b[off+2 : off+2+int(n)]), true
}
