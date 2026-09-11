package apk

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

const testPackInfo = `{
  "summary": {
    "app": {
      "bundleName": "com.example.harmony",
      "bundleType": "app",
      "version": {"code": 1000003, "name": "1.0.3"}
    },
    "modules": [{"distro": {"moduleType": "entry", "moduleName": "entry"}}]
  },
  "packages": [{"deviceType": ["phone"], "moduleType": "entry", "deliveryWithInstall": true, "name": "entry-default"}]
}`

func writeZipEntries(t *testing.T, path string, entries map[string][]byte, method uint16) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func zipBytes(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "inner.zip")
	writeZipEntries(t, p, entries, zip.Deflate)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func le32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }
func le16(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }

// v1String is one STRING entry of a v1 (Restool) resources.index key.
type v1String struct {
	id    uint32
	name  string
	value string
}

// buildIndexV1 lays out a Restool-format resources.index with one key
// per qualifier set. params is the (type,value) list for each key.
func buildIndexV1(keys [][][2]uint32, strs [][]v1String) []byte {
	var out bytes.Buffer
	ver := make([]byte, 128)
	copy(ver, "Restool 4.0.0")
	out.Write(ver)
	lenPos := out.Len()
	out.Write(le32(0)) // length placeholder
	out.Write(le32(uint32(len(keys))))

	// Key headers need the IDSS offsets, which depend on the size of all
	// key headers; compute the header section size first.
	hdrSize := out.Len()
	for _, k := range keys {
		hdrSize += 12 + 8*len(k)
	}
	// Precompute per-key IDSS block and item bytes.
	type block struct{ ids, items []byte }
	blocks := make([]block, len(keys))
	off := hdrSize
	for i := range keys {
		var ids, items bytes.Buffer
		ids.WriteString("IDSS")
		ids.Write(le32(uint32(len(strs[i]))))
		idsSize := 8 + 8*len(strs[i])
		itemBase := off + idsSize
		for _, s := range strs[i] {
			ids.Write(le32(s.id))
			ids.Write(le32(uint32(itemBase + items.Len())))
			var it bytes.Buffer
			it.Write(le32(0)) // size (unused)
			it.Write(le32(resTypeString))
			it.Write(le32(s.id))
			it.Write(le16(uint16(len(s.value) + 1)))
			it.WriteString(s.value)
			it.WriteByte(0)
			it.Write(le16(uint16(len(s.name) + 1)))
			it.WriteString(s.name)
			it.WriteByte(0)
			items.Write(it.Bytes())
		}
		blocks[i] = block{ids.Bytes(), items.Bytes()}
		off += idsSize + items.Len()
	}
	// Now write key headers.
	off = hdrSize
	for i, k := range keys {
		out.WriteString("KEYS")
		out.Write(le32(uint32(off)))
		out.Write(le32(uint32(len(k))))
		for _, p := range k {
			out.Write(le32(p[0]))
			out.Write(le32(p[1]))
		}
		off += len(blocks[i].ids) + len(blocks[i].items)
	}
	for _, b := range blocks {
		out.Write(b.ids)
		out.Write(b.items)
	}
	b := out.Bytes()
	binary.LittleEndian.PutUint32(b[lenPos:], uint32(len(b)))
	return b
}

// buildIndexV2 lays out a new-format resources.index: keys with config
// ids, an IDSS block with one STRING type, and a data block where each
// id has one value per config.
func buildIndexV2(cfgs map[uint32][][2]uint32, ids []struct {
	id     uint32
	name   string
	values map[uint32]string // cfgId → value
}) []byte {
	var out bytes.Buffer
	ver := make([]byte, 128)
	copy(ver, "Resource 5.0.0")
	out.Write(ver)
	lenPos := out.Len()
	out.Write(le32(0))
	out.Write(le32(uint32(len(cfgs))))
	dataPos := out.Len()
	out.Write(le32(0))
	// keys (deterministic order)
	for cfgID := uint32(0); cfgID < 64; cfgID++ {
		params, ok := cfgs[cfgID]
		if !ok {
			continue
		}
		out.WriteString("KEYS")
		out.Write(le32(cfgID))
		out.Write(le32(uint32(len(params))))
		for _, p := range params {
			out.Write(le32(p[0]))
			out.Write(le32(p[1]))
		}
	}
	// IDSS: header + one TypeInfo (STRING) + items. Item infoOffsets point
	// into the data block, which we lay out after computing the IDSS size.
	idssSize := 16 + 12
	for _, it := range ids {
		idssSize += 12 + len(it.name)
	}
	dataOff := out.Len() + idssSize
	// Precompute data block.
	var data bytes.Buffer
	infoOffsets := make([]uint32, len(ids))
	for i, it := range ids {
		infoOffsets[i] = uint32(dataOff + data.Len())
		// ResInfo + ConfigItems + values
		var vals bytes.Buffer
		var cfgItems bytes.Buffer
		valBase := dataOff + data.Len() + 12 + 8*len(it.values)
		for cfgID := uint32(0); cfgID < 64; cfgID++ {
			v, ok := it.values[cfgID]
			if !ok {
				continue
			}
			cfgItems.Write(le32(cfgID))
			cfgItems.Write(le32(uint32(valBase + vals.Len())))
			vals.Write(le16(uint16(len(v))))
			vals.WriteString(v)
		}
		data.Write(le32(it.id))
		data.Write(le32(uint32(12 + cfgItems.Len() + vals.Len())))
		data.Write(le32(uint32(len(it.values))))
		data.Write(cfgItems.Bytes())
		data.Write(vals.Bytes())
	}
	out.WriteString("IDSS")
	out.Write(le32(uint32(idssSize)))
	out.Write(le32(1))
	out.Write(le32(uint32(len(ids))))
	typeLen := 12
	for _, it := range ids {
		typeLen += 12 + len(it.name)
	}
	out.Write(le32(resTypeString))
	out.Write(le32(uint32(typeLen)))
	out.Write(le32(uint32(len(ids))))
	for i, it := range ids {
		out.Write(le32(it.id))
		out.Write(le32(infoOffsets[i]))
		out.Write(le32(uint32(len(it.name))))
		out.WriteString(it.name)
	}
	if out.Len() != dataOff {
		panic("test fixture: data offset mismatch")
	}
	out.Write(data.Bytes())
	b := out.Bytes()
	binary.LittleEndian.PutUint32(b[lenPos:], uint32(len(b)))
	binary.LittleEndian.PutUint32(b[dataPos:], uint32(dataOff))
	return b
}

func TestIsHarmony(t *testing.T) {
	for _, c := range []struct {
		p    string
		want bool
	}{
		{"a.app", true}, {"A.APP", true}, {"entry-default-signed.hap", true},
		{"a.apk", false}, {"a.aab", false}, {"a.ipa", false}, {"noext", false},
	} {
		if got := IsHarmony(c.p); got != c.want {
			t.Errorf("IsHarmony(%q) = %v, want %v", c.p, got, c.want)
		}
	}
	if !IsHarmonyAppPack("x.app") || IsHarmonyAppPack("x.hap") {
		t.Error("IsHarmonyAppPack wrong")
	}
}

func TestParseHarmonyHapV1Index(t *testing.T) {
	// zh-CN qualifier listed first to prove the base one wins.
	idx := buildIndexV1(
		[][][2]uint32{{{keyTypeLanguages, 0x7a68}, {keyTypeRegion, 0x434e}}, {}},
		[][]v1String{
			{{id: 16777216, name: "app_name", value: "鸿蒙示例"}},
			{{id: 16777216, name: "app_name", value: "Harmony Demo"}, {id: 16777217, name: "other", value: "x"}},
		},
	)
	dir := t.TempDir()
	hap := filepath.Join(dir, "entry-default-signed.hap")
	writeZipEntries(t, hap, map[string][]byte{
		"pack.info":       []byte(testPackInfo),
		"module.json":     []byte(`{"app":{"bundleName":"com.example.harmony","label":"$string:app_name"},"module":{"name":"entry","type":"entry"}}`),
		"resources.index": idx,
	}, zip.Deflate)

	info, err := ParseHarmony(hap)
	if err != nil {
		t.Fatal(err)
	}
	if info.Platform != PlatformHarmony {
		t.Errorf("platform = %q", info.Platform)
	}
	if info.PackageName != "com.example.harmony" || info.VersionName != "1.0.3" || info.VersionCode != 1000003 {
		t.Errorf("unexpected info: %+v", info)
	}
	if info.AppName != "Harmony Demo" {
		t.Errorf("AppName = %q, want base-locale label", info.AppName)
	}
}

func TestParseHarmonyAppPackV2IndexByID(t *testing.T) {
	idx := buildIndexV2(
		map[uint32][][2]uint32{0: {}, 1: {{keyTypeLanguages, 0x7a68}}},
		[]struct {
			id     uint32
			name   string
			values map[uint32]string
		}{
			{id: 16777216, name: "app_name", values: map[uint32]string{0: "Pack Demo", 1: "打包示例"}},
			{id: 16777217, name: "greeting", values: map[uint32]string{0: "hi"}},
		},
	)
	inner := zipBytes(t, map[string][]byte{
		"pack.info":       []byte(testPackInfo),
		"module.json":     []byte(`{"app":{"label":"$string:16777216"}}`),
		"resources.index": idx,
	})
	dir := t.TempDir()
	app := filepath.Join(dir, "demo-default-signed.app")
	writeZipEntries(t, app, map[string][]byte{
		"pack.info":         []byte(testPackInfo),
		"entry-default.hap": inner,
	}, zip.Store)

	info, err := ParseHarmony(app)
	if err != nil {
		t.Fatal(err)
	}
	if info.PackageName != "com.example.harmony" || info.VersionCode != 1000003 {
		t.Errorf("unexpected info: %+v", info)
	}
	if info.AppName != "Pack Demo" {
		t.Errorf("AppName = %q, want %q", info.AppName, "Pack Demo")
	}
}

func TestParseHarmonyWithoutLabelStillWorks(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "x.app")
	writeZipEntries(t, app, map[string][]byte{"pack.info": []byte(testPackInfo)}, zip.Deflate)
	info, err := ParseHarmony(app)
	if err != nil {
		t.Fatal(err)
	}
	if info.AppName != "" || info.PackageName != "com.example.harmony" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestParseHarmonyRejectsNonHarmonyZip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.app")
	writeZipEntries(t, p, map[string][]byte{"README": []byte("nope")}, zip.Deflate)
	if _, err := ParseHarmony(p); err == nil {
		t.Fatal("expected error for zip without pack.info")
	}
	if _, err := ParseHarmony(filepath.Join(dir, "missing.app")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLookupIndexStringRejectsGarbage(t *testing.T) {
	if _, ok := lookupIndexString([]byte("short"), "app_name", 0); ok {
		t.Fatal("expected failure on short input")
	}
	junk := make([]byte, 200)
	copy(junk, "Restool 1")
	if _, ok := lookupIndexString(junk, "app_name", 0); ok {
		t.Fatal("expected failure on zero keys")
	}
}
