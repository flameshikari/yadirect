// Install the pinned official UPX release without requiring tar/xz packages.
package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ulikunitz/xz"
)

const version = "5.3.0"

var checksums = map[string]string{
	"amd64": "79cea8a50b6fa102d10c7b79a419cd77dead3cc2d78661de2dc5e5815dc505c1",
	"arm64": "2a4afa860ac7aa6968f88df7841823746e5d00ba56affdf9342c2f5254fb776f",
}

func extractBinary(archive []byte, checksum, member string) ([]byte, error) {
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != checksum {
		return nil, fmt.Errorf("UPX archive SHA-256 mismatch")
	}
	compressed, err := xz.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("UPX binary not found in archive")
			}
			return nil, err
		}
		if header.Name != member || header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size <= 0 || header.Size > 16<<20 {
			return nil, fmt.Errorf("invalid UPX binary size")
		}
		return io.ReadAll(io.LimitReader(reader, 16<<20))
	}
}
func install(output string) error {
	checksum, ok := checksums[runtime.GOARCH]
	if runtime.GOOS != "linux" || !ok {
		return fmt.Errorf("automatic UPX install supports Linux amd64/arm64; install UPX manually or set UPX_BINARY")
	}
	bundle := fmt.Sprintf("upx-%s-%s_linux", version, runtime.GOARCH)
	client := http.Client{Timeout: 60 * time.Second}
	response, err := client.Get(fmt.Sprintf("https://github.com/upx/upx/releases/download/v%s/%s.tar.xz", version, bundle))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("UPX download HTTP %d", response.StatusCode)
	}
	archive, err := io.ReadAll(io.LimitReader(response.Body, (10<<20)+1))
	if err != nil {
		return err
	}
	if len(archive) > 10<<20 {
		return fmt.Errorf("UPX archive exceeds size limit")
	}
	binary, err := extractBinary(archive, checksum, bundle+"/upx")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".upx-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err = temporary.Write(binary); err != nil {
		temporary.Close()
		return err
	}
	if err = temporary.Chmod(0755); err != nil {
		temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, output)
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/upx OUTPUT_PATH")
		os.Exit(2)
	}
	if err := install(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Installed verified UPX %s: %s\n", version, os.Args[1])
}
