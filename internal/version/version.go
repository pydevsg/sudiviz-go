// Package version holds the CLI identity shared by every output mode.
package version

import "fmt"

// Version is overridden at link time by GoReleaser:
//
//	-ldflags "-X github.com/pydevsg/sudiviz-go/internal/version.Version=v1.0.0"
var Version = "1.0.0"

const (
	// Name is the binary / product name.
	Name = "sudiviz"
	// Tagline is printed under the logo.
	Tagline = "X-ray vision for your cloud infrastructure"
)

// Logo is the ASCII wordmark shown by diagnose / fix / version / watch.
const Logo = `
                    _ _       _
   ___ _   _  __| (_)_   _(_)____
  / __| | | |/ _` + "`" + ` | \ \ / / |_  /
  \__ \ |_| | (_| | |\ V /| |/ /
  |___/\__,_|\__,_|_| \_/ |_/___|

  X-ray vision for your cloud infrastructure
`

// Banner returns the logo plus version line.
func Banner() string {
	return fmt.Sprintf("%s\n  v%s\n", Logo, Version)
}
