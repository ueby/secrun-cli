//go:build unix

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ueby/secrun-cli/internal/fakesm"
)

// TestEndToEnd compila o binário real e o executa contra um Secrets Manager
// falso, validando injeção de variáveis, limpeza de SECRUN_*, exec do
// comando e propagação do código de saída.
func TestEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e ignorado em -short")
	}

	bin := buildBinary(t)

	srv := httptest.NewServer(fakesm.Handler(map[string]string{
		"shared": `{"DB_HOST":"shared-db","LOG_LEVEL":"info"}`,
		"app":    `{"DB_HOST":"app-db","PORT":8080}`,
		"bad":    `{"K": SUPERSECRETO}`,
		"danger": `{"NODE_OPTIONS":"--require /tmp/x.js"}`,
	}))
	t.Cleanup(srv.Close)

	baseEnv := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"AWS_ENDPOINT_URL_SECRETS_MANAGER=" + srv.URL,
		"AWS_ACCESS_KEY_ID=test",
		"AWS_SECRET_ACCESS_KEY=test",
		"AWS_REGION=us-east-1",
		"AWS_EC2_METADATA_DISABLED=true",
	}

	run := func(env []string, args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = append(append([]string{}, baseEnv...), env...)
		out, err := cmd.CombinedOutput()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("execução: %v", err)
		}
		return string(out), code
	}

	t.Run("injeta e mescla na ordem", func(t *testing.T) {
		out, code := run([]string{"SECRUN_IDS=shared,app"},
			"run", "--prefix", "APP_", "--", "sh", "-c", `env | grep -E '^(APP_|SECRUN_)' | sort`)
		if code != 0 {
			t.Fatalf("código %d: %s", code, out)
		}
		want := "APP_DB_HOST=app-db\nAPP_LOG_LEVEL=info\nAPP_PORT=8080\n"
		if out != want {
			t.Errorf("saída =\n%s\nesperado\n%s", out, want)
		}
	})

	t.Run("propaga código de saída da aplicação", func(t *testing.T) {
		if _, code := run(nil, "run", "--secret-id", "app", "--", "sh", "-c", "exit 42"); code != 42 {
			t.Errorf("código = %d, esperado 42", code)
		}
	})

	t.Run("erro de JSON não vaza conteúdo", func(t *testing.T) {
		out, code := run(nil, "run", "--secret-id", "bad", "--", "true")
		if code != 1 || strings.Contains(out, "SUPERSECRETO") {
			t.Errorf("código %d, saída: %s", code, out)
		}
	})

	t.Run("variável perigosa é bloqueada", func(t *testing.T) {
		out, code := run(nil, "run", "--secret-id", "danger", "--", "true")
		if code != 1 || !strings.Contains(out, "bloqueada") {
			t.Errorf("código %d, saída: %s", code, out)
		}
	})

	t.Run("arquivo sem permissão de execução retorna 126", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "nao-executavel")
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, code := run(nil, "run", "--secret-id", "app", "--", f); code != 126 {
			t.Errorf("código %d, esperado 126: %s", code, out)
		}
	})

	t.Run("segredo inexistente", func(t *testing.T) {
		out, code := run(nil, "run", "--secret-id", "nope", "--", "true")
		if code != 1 || !strings.Contains(out, "ResourceNotFoundException") {
			t.Errorf("código %d, saída: %s", code, out)
		}
	})
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "secrun")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func awsTestEnv(endpoint string, t *testing.T) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"AWS_ENDPOINT_URL_SECRETS_MANAGER=" + endpoint,
		"AWS_ACCESS_KEY_ID=test",
		"AWS_SECRET_ACCESS_KEY=test",
		"AWS_REGION=us-east-1",
		"AWS_EC2_METADATA_DISABLED=true",
	}
}

// TestEndToEndRetryOnHungConnection garante que uma tentativa travada é
// abortada pelo timeout por tentativa e que o retry obtém o segredo dentro
// do prazo total.
func TestEndToEndRetryOnHungConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e ignorado em -short")
	}
	bin := buildBinary(t)

	var calls atomic.Int32
	ok := fakesm.Handler(map[string]string{"app": `{"K":"v"}`})
	srv := hangingServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		if calls.Add(1) == 1 {
			return true // primeira tentativa nunca responde
		}
		ok.ServeHTTP(w, r)
		return false
	})

	cmd := exec.Command(bin, "run", "--timeout", "3s", "--secret-id", "app", "--", "sh", "-c", `test "$K" = v`)
	cmd.Env = awsTestEnv(srv.URL, t)
	start := time.Now()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("esperado sucesso após retry: %v\n%s", err, out)
	}
	if n := calls.Load(); n < 2 {
		t.Errorf("tentativas = %d, esperado >= 2", n)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("levou %s; o timeout por tentativa deveria ter agido antes", elapsed)
	}
}

// TestEndToEndSIGTERMDuringFetch garante que um SIGTERM durante a busca
// encerra o secrun prontamente com 143, sem iniciar a aplicação.
func TestEndToEndSIGTERMDuringFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e ignorado em -short")
	}
	bin := buildBinary(t)

	srv := hangingServer(t, func(http.ResponseWriter, *http.Request) bool { return true })

	marker := filepath.Join(t.TempDir(), "app-started")
	cmd := exec.Command(bin, "run", "--timeout", "30s", "--secret-id", "app", "--", "touch", marker)
	cmd.Env = awsTestEnv(srv.URL, t)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 143 {
			t.Errorf("esperado código 143, obtido: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("secrun não encerrou após SIGTERM")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a aplicação não deveria ter sido iniciada")
	}
}

// hangingServer cria um servidor em que handle decide se a requisição deve
// ficar pendurada (sem resposta) até o fim do teste.
func hangingServer(t *testing.T, handle func(http.ResponseWriter, *http.Request) bool) *httptest.Server {
	t.Helper()
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handle(w, r) {
			select {
			case <-r.Context().Done():
			case <-stop:
			}
		}
	}))
	t.Cleanup(func() {
		close(stop)
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv
}
