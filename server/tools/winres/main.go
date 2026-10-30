// Generate Windows executable resources using the project's existing favicon.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func resources(iconPath, release string) (*winres.ResourceSet, error) {
	parts := regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:[-+][0-9A-Za-z.-]+)?$`).FindStringSubmatch(release)
	if parts == nil {
		return nil, fmt.Errorf("invalid release version: %q", release)
	}
	var numbers [4]uint16
	for i := 0; i < 3; i++ {
		value, err := strconv.ParseUint(parts[i+1], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid Windows version: %w", err)
		}
		numbers[i] = uint16(value)
	}
	iconFile, err := os.Open(iconPath)
	if err != nil {
		return nil, err
	}
	defer iconFile.Close()
	icon, err := winres.LoadICO(iconFile)
	if err != nil {
		return nil, err
	}
	rs := &winres.ResourceSet{}
	if err = rs.SetIcon(winres.ID(1), icon); err != nil {
		return nil, err
	}
	vi := version.Info{FileVersion: numbers, ProductVersion: numbers, Type: version.App}
	for key, value := range map[string]string{
		version.ProductName:      "Yadirect",
		version.FileDescription:  "Yadirect",
		version.CompanyName:      "flameshikari",
		version.InternalName:     "yadirect",
		version.OriginalFilename: "yadirect.exe",
		version.FileVersion:      release,
		version.ProductVersion:   release,
		version.Comments:         "https://github.com/flameshikari/yadirect",
		version.LegalCopyright:   "© 2026 flameshikari",
	} {
		if err = vi.Set(version.LangDefault, key, value); err != nil {
			return nil, err
		}
	}
	rs.SetVersionInfo(vi)
	rs.SetManifest(winres.AppManifest{Description: "Yadirect", Identity: winres.AssemblyIdentity{Name: "flameshikari.yadirect", Version: numbers}, Compatibility: winres.Win10AndAbove, ExecutionLevel: winres.AsInvoker, LongPathAware: true})
	return rs, nil
}
func generate(iconPath, release, arch, output string) error {
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported architecture: %s", arch)
	}
	rs, err := resources(iconPath, release)
	if err != nil {
		return err
	}
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	if err = rs.WriteObject(file, winres.Arch(arch)); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/winres ICON VERSION ARCH OUTPUT")
		os.Exit(2)
	}
	if err := generate(os.Args[1], os.Args[2], os.Args[3], os.Args[4]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
