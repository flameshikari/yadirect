package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/ulikunitz/xz"
	"testing"
)

func TestVerifiedExtraction(t *testing.T) {
	var data bytes.Buffer
	compressor, err := xz.NewWriter(&data)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewWriter(compressor)
	body := []byte("test executable")
	if err = archive.WriteHeader(&tar.Header{Name: "bundle/upx", Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = archive.Write(body)
	_ = archive.Close()
	_ = compressor.Close()
	checksum := fmt.Sprintf("%x", sha256.Sum256(data.Bytes()))
	result, err := extractBinary(data.Bytes(), checksum, "bundle/upx")
	if err != nil || !bytes.Equal(result, body) {
		t.Fatal(string(result), err)
	}
	if _, err = extractBinary(data.Bytes(), "wrong checksum", "bundle/upx"); err == nil {
		t.Fatal("unverified archive accepted")
	}
	if _, err = extractBinary(data.Bytes(), checksum, "missing/upx"); err == nil {
		t.Fatal("missing member accepted")
	}
}
