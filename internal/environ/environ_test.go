package environ

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestBuild(t *testing.T) {
	t.Parallel()

	current := []string{
		"PATH=/usr/bin",
		"DB_HOST=from-container",
		"DB_HOST=duplicada-ignorada",
		"EQUALS=a=b=c",
		"SECRUN_IDS=secrun/app/prod",
		"SECRUN_ROLE_ARN=arn:aws:iam::1:role/x",
		"SECRUN_EXTERNAL_ID=ext",
		"SEM_IGUAL",
		"=C:",
	}
	secretValues := map[string]string{"DB_HOST": "from-secret", "API_KEY": "k"}

	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{
			name: "override ativo",
			opts: Options{Override: true},
			want: []string{"API_KEY=k", "DB_HOST=from-secret", "EQUALS=a=b=c", "PATH=/usr/bin"},
		},
		{
			name: "override desativado preserva o ambiente (primeira ocorrência)",
			opts: Options{Override: false},
			want: []string{"API_KEY=k", "DB_HOST=from-container", "EQUALS=a=b=c", "PATH=/usr/bin"},
		},
		{
			name: "prefixo",
			opts: Options{Override: true, Prefix: "APP_"},
			want: []string{"APP_API_KEY=k", "APP_DB_HOST=from-secret", "DB_HOST=from-container", "EQUALS=a=b=c", "PATH=/usr/bin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Build(current, secretValues, tt.opts)
			if err != nil {
				t.Fatalf("Build() erro inesperado: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Build() =\n  %v\nesperado\n  %v", got, tt.want)
			}
		})
	}
}

func TestBuildRejectsInvalidKeys(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"A=B", "A\nB"} {
		if _, err := Build(nil, map[string]string{key: "x"}, Options{Override: true}); err == nil {
			t.Errorf("Build() deveria rejeitar a chave %q", key)
		}
	}
}

func TestBuildDenyPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		secrets map[string]string
		opts    Options
		denied  bool
	}{
		{name: "NODE_OPTIONS", secrets: map[string]string{"NODE_OPTIONS": "--require /tmp/x.js"}, denied: true},
		{name: "LD_PRELOAD", secrets: map[string]string{"LD_PRELOAD": "/tmp/x.so"}, denied: true},
		{name: "BASH_FUNC_", secrets: map[string]string{"BASH_FUNC_ls%%": "() { id; }"}, denied: true},
		{name: "PATH", secrets: map[string]string{"PATH": "/tmp"}, denied: true},
		{name: "minúsculas", secrets: map[string]string{"https_proxy": "http://evil"}, denied: true},
		{name: "endpoint AWS", secrets: map[string]string{"AWS_ENDPOINT_URL_S3": "http://evil"}, denied: true},
		{name: "credenciais de container AWS", secrets: map[string]string{"AWS_CONTAINER_CREDENTIALS_FULL_URI": "http://evil"}, denied: true},
		{name: "prefixo reservado", secrets: map[string]string{"SECRUN_ROLE_ARN": "arn"}, denied: true},
		{name: "prefixo reservado mesmo com allow", secrets: map[string]string{"SECRUN_X": "1"},
			opts: Options{AllowKeys: []string{"SECRUN_X"}}, denied: true},
		{name: "prefixo que forma nome bloqueado", secrets: map[string]string{"PRELOAD": "x"},
			opts: Options{Prefix: "LD_"}, denied: true},
		{name: "allow-key libera", secrets: map[string]string{"NODE_OPTIONS": "--max-old-space-size=512"},
			opts: Options{AllowKeys: []string{"NODE_OPTIONS"}}},
		{name: "prefixo neutraliza", secrets: map[string]string{"NODE_OPTIONS": "x"},
			opts: Options{Prefix: "APP_"}},
		{name: "AWS_ comum é permitida", secrets: map[string]string{"AWS_S3_BUCKET": "b", "AWS_REGION_NAME": "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.opts.Override = true
			_, err := Build([]string{"PATH=/usr/bin"}, tt.secrets, tt.opts)
			if got := errors.Is(err, ErrDeniedKey); got != tt.denied {
				t.Errorf("bloqueado = %v (erro: %v), esperado %v", got, err, tt.denied)
			}
		})
	}
}

func TestDeniedNamesDocumented(t *testing.T) {
	t.Parallel()

	names := DeniedNames()
	for _, n := range []string{"LD_*", "NODE_OPTIONS", "PATH", "AWS_ENDPOINT_URL*"} {
		if !slices.Contains(names, n) {
			t.Errorf("DeniedNames() não contém %q", n)
		}
	}
	for _, n := range names {
		if n != strings.ToUpper(n) {
			t.Errorf("nome bloqueado deve estar em maiúsculas: %q", n)
		}
	}
}

func TestValidatePrefix(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"", "APP_", "x."} {
		if err := ValidatePrefix(p); err != nil {
			t.Errorf("ValidatePrefix(%q) = %v", p, err)
		}
	}
	for _, p := range []string{"A=", "A\x00", "A\n", "SECRUN_", "secrun_x"} {
		if err := ValidatePrefix(p); err == nil {
			t.Errorf("ValidatePrefix(%q) deveria falhar", p)
		}
	}
}

// TestDenyListDocumented garante que o SECURITY.md lista todas as variáveis
// bloqueadas, mantendo documentação e código sincronizados.
func TestDenyListDocumented(t *testing.T) {
	t.Parallel()

	doc, err := os.ReadFile("../../SECURITY.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range DeniedNames() {
		if !strings.Contains(string(doc), "`"+name+"`") {
			t.Errorf("SECURITY.md não documenta %q", name)
		}
	}
}
