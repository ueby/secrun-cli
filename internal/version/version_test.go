package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetUsesLdflagsValues(t *testing.T) {
	origV, origC, origD := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = origV, origC, origD })

	Version, Commit, Date = "v1.2.3", "0123456789abcdef0123", "2026-01-02T03:04:05Z"
	info := Get()

	if info.Version != "v1.2.3" || info.Date != "2026-01-02T03:04:05Z" {
		t.Errorf("Get() = %+v", info)
	}
	if info.Commit != "0123456789ab" {
		t.Errorf("Commit deveria ser abreviado para 12 caracteres: %q", info.Commit)
	}
	if info.GoVersion != runtime.Version() || info.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Errorf("metadados de runtime incorretos: %+v", info)
	}
}

func TestStringFormat(t *testing.T) {
	t.Parallel()

	s := Info{Version: "v1.2.3", Commit: "abc", Date: "d", GoVersion: "go1.27.4", Platform: "linux/amd64"}.String()
	want := "secrun v1.2.3 (commit abc, build d, go1.27.4, linux/amd64)"
	if s != want {
		t.Errorf("String() = %q, esperado %q", s, want)
	}
}

func TestGetDefaults(t *testing.T) {
	origV, origC, origD := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = origV, origC, origD })

	Version, Commit, Date = "dev", "", ""
	info := Get()
	if info.Version == "" || info.Commit == "" || info.Date == "" {
		t.Errorf("campos não deveriam ficar vazios: %+v", info)
	}
	if !strings.HasPrefix(info.String(), "secrun ") {
		t.Errorf("String() = %q", info.String())
	}
}
