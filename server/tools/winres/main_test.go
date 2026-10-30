package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func testIcon(t *testing.T) string {
	t.Helper()
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewNRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 22)
	binary.LittleEndian.PutUint16(data[2:], 1)
	binary.LittleEndian.PutUint16(data[4:], 1)
	data[6] = 16
	data[7] = 16
	binary.LittleEndian.PutUint16(data[10:], 1)
	binary.LittleEndian.PutUint16(data[12:], 32)
	binary.LittleEndian.PutUint32(data[14:], uint32(picture.Len()))
	binary.LittleEndian.PutUint32(data[18:], 22)
	path := filepath.Join(t.TempDir(), "favicon.ico")
	if err := os.WriteFile(path, append(data, picture.Bytes()...), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestWindowsResources(t *testing.T) {
	icon := testIcon(t)
	rs, err := resources(icon, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.GetIcon(winres.ID(1)); err != nil {
		t.Fatal(err)
	}
	vi, err := version.FromBytes(rs.Get(winres.RT_VERSION, winres.ID(1), version.LangDefault))
	if err != nil {
		t.Fatal(err)
	}
	if vi.FileVersion != [4]uint16{1, 2, 3, 0} || vi.ProductVersion != vi.FileVersion {
		t.Fatal("wrong numeric version")
	}
	table := *vi.Table()[version.LangDefault]
	if table[version.FileDescription] != "Yadirect" || table[version.LegalCopyright] != "© 2026 flameshikari" || table[version.ProductName] != "Yadirect" || table[version.OriginalFilename] != "yadirect.exe" || table[version.FileVersion] != "1.2.3" || table[version.CompanyName] != "flameshikari" {
		t.Fatalf("metadata: %v", table)
	}
	manifest := string(rs.Get(winres.RT_MANIFEST, winres.ID(1), winres.LCIDDefault))
	if !strings.Contains(manifest, `level="asInvoker"`) {
		t.Fatal("manifest must not request administrator access")
	}
	for arch, machine := range map[string]uint16{"amd64": pe.IMAGE_FILE_MACHINE_AMD64, "arm64": pe.IMAGE_FILE_MACHINE_ARM64} {
		out := filepath.Join(t.TempDir(), "resources.syso")
		if err := generate(icon, "1.2.3", arch, out); err != nil {
			t.Fatal(err)
		}
		file, err := pe.Open(out)
		if err != nil {
			t.Fatal(err)
		}
		if file.Machine != machine {
			t.Fatal("wrong COFF architecture")
		}
		file.Close()
	}
	for _, release := range []string{"dev", "1.2", "65536.0.0"} {
		if _, err := resources(icon, release); err == nil {
			t.Fatalf("accepted invalid version: %s", release)
		}
	}
}
