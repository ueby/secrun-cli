// Package version expõe os metadados de build do secrun.
//
// Os valores são injetados em tempo de compilação via -ldflags:
//
//	go build -ldflags "-X github.com/ueby/secrun-cli/internal/version.Version=v1.0.0 \
//	  -X github.com/ueby/secrun-cli/internal/version.Commit=$(git rev-parse HEAD) \
//	  -X github.com/ueby/secrun-cli/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// Quando não informados, são usadas as informações de VCS embutidas pelo
// toolchain do Go (runtime/debug.ReadBuildInfo), quando disponíveis.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Variáveis preenchidas via -ldflags no momento do build.
var (
	// Version é a versão semântica do binário (ex.: v1.0.0).
	Version = "dev"
	// Commit é o hash do commit Git de origem.
	Commit = ""
	// Date é a data/hora (UTC, RFC 3339) do build.
	Date = ""
)

// Info agrupa os metadados de build resolvidos.
type Info struct {
	Version   string
	Commit    string
	Date      string
	GoVersion string
	Platform  string
}

// Get resolve os metadados de build, completando lacunas com as
// informações de VCS embutidas pelo toolchain.
func Get() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			}
		}
	}

	if len(info.Commit) > 12 {
		info.Commit = info.Commit[:12]
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.Date == "" {
		info.Date = "unknown"
	}
	return info
}

// String retorna uma linha legível com os metadados de build.
func (i Info) String() string {
	return fmt.Sprintf("secrun %s (commit %s, build %s, %s, %s)",
		i.Version, i.Commit, i.Date, i.GoVersion, i.Platform)
}
