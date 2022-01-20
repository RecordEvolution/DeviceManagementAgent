package release

import (
	_ "embed"
	"runtime"
	"strings"
)

//go:embed version.txt
var version string
var BuildArch string = ""

func GetSystemInfo() (string, string, string) {
	arch := BuildArch
	variant := ""

	if arch == "" {
		arch = "amd64"
	}

	if strings.Contains(arch, "arm") {
		arch = "arm"

		splitArmArch := strings.Split(arch, "v")
		if len(splitArmArch) == 2 {
			variant = "v" + splitArmArch[1]
		}
	}

	return runtime.GOOS, arch, variant
}

func GetVersion() string {
	return version
}

func GetBuildArch() string {
	return BuildArch
}
