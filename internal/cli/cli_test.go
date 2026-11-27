package cli

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ueby/secrun-cli/internal/awsconfig"
)

type fakeFetcher struct {
	values map[string]string
	err    error
}

func (f fakeFetcher) FetchAll(context.Context, []string) (map[string]string, error) {
	return f.values, f.err
}

// execCall registra a chamada que substituiria o processo.
type execCall struct {
	argv0 string
	argv  []string
	env   []string
}

type harness struct {
	app    *App
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	exec   *execCall
	opts   *awsconfig.Options
}

func newHarness(env map[string]string, fetcher SecretFetcher) *harness {
	h := &harness{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	h.app = &App{
		Stdout: h.stdout,
		Stderr: h.stderr,
		Getenv: func(k string) string { return env[k] },
		Environ: func() []string {
			out := make([]string, 0, len(env))
			for k, v := range env {
				out = append(out, k+"="+v)
			}
			return out
		},
		LookPath: func(file string) (string, error) {
			switch file {
			case "missing":
				return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
			case "/missing":
				return "", &exec.Error{Name: file, Err: fs.ErrNotExist}
			case "/etc/hosts":
				return "", &exec.Error{Name: file, Err: fs.ErrPermission}
			}
			return "/usr/bin/" + file, nil
		},
		Exec: func(argv0 string, argv, envv []string) error {
			h.exec = &execCall{argv0, argv, envv}
			return nil
		},
		NewFetcher: func(_ context.Context, opts awsconfig.Options) (SecretFetcher, error) {
			h.opts = &opts
			return fetcher, nil
		},
	}
	return h
}

func TestRunExecsWithInjectedEnv(t *testing.T) {
	t.Parallel()

	env := map[string]string{"PATH": "/usr/bin", "SECRUN_IDS": "secrun/app/prod", "SECRUN_ROLE_ARN": "arn:role"}
	h := newHarness(env, fakeFetcher{values: map[string]string{"DB_PASSWORD": "s3cr3t"}})

	code := h.app.Run(t.Context(), []string{"run", "--region", "us-east-1", "--", "node", "server.js"})
	if code != ExitOK {
		t.Fatalf("Run() = %d, stderr: %s", code, h.stderr)
	}
	if h.exec == nil {
		t.Fatal("Exec não foi chamado")
	}
	if h.exec.argv0 != "/usr/bin/node" || !slices.Equal(h.exec.argv, []string{"node", "server.js"}) {
		t.Errorf("exec = %+v", h.exec)
	}
	wantEnv := []string{"DB_PASSWORD=s3cr3t", "PATH=/usr/bin"}
	if !slices.Equal(h.exec.env, wantEnv) {
		t.Errorf("env = %v, esperado %v", h.exec.env, wantEnv)
	}
	if h.opts.Region != "us-east-1" || h.opts.RoleARN != "arn:role" {
		t.Errorf("opções AWS = %+v", *h.opts)
	}
	if strings.Contains(h.stderr.String(), "s3cr3t") {
		t.Error("valor do segredo apareceu no stderr")
	}
}

func TestRunVerboseNeverLogsValues(t *testing.T) {
	t.Parallel()

	h := newHarness(nil, fakeFetcher{values: map[string]string{"DB_PASSWORD": "s3cr3t"}})
	code := h.app.Run(t.Context(), []string{"run", "--verbose", "--secret-id", "a", "--", "app"})
	if code != ExitOK {
		t.Fatalf("Run() = %d, stderr: %s", code, h.stderr)
	}
	logs := h.stderr.String()
	if !strings.Contains(logs, "injected_vars=1") {
		t.Errorf("log verbose esperado, obtido: %s", logs)
	}
	if strings.Contains(logs, "s3cr3t") || strings.Contains(logs, "DB_PASSWORD") {
		t.Errorf("log verbose expõe segredo: %s", logs)
	}
}

func TestRunExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		fetcher SecretFetcher
		want    int
	}{
		{name: "sem argumentos", args: nil, want: ExitUsage},
		{name: "comando desconhecido", args: []string{"nope"}, want: ExitUsage},
		{name: "help", args: []string{"help"}, want: ExitOK},
		{name: "version", args: []string{"version"}, want: ExitOK},
		{name: "run help", args: []string{"run", "--help"}, want: ExitOK},
		{name: "uso incorreto", args: []string{"run", "--", "app"}, want: ExitUsage},
		{name: "falha na AWS", args: []string{"run", "--secret-id", "a", "--", "app"},
			fetcher: fakeFetcher{err: context.DeadlineExceeded}, want: ExitFailure},
		{name: "chave inválida", args: []string{"run", "--secret-id", "a", "--", "app"},
			fetcher: fakeFetcher{values: map[string]string{"A=B": "x"}}, want: ExitFailure},
		{name: "executável inexistente", args: []string{"run", "--secret-id", "a", "--", "missing"},
			fetcher: fakeFetcher{values: map[string]string{}}, want: ExitNotFound},
		{name: "caminho absoluto inexistente", args: []string{"run", "--secret-id", "a", "--", "/missing"},
			fetcher: fakeFetcher{values: map[string]string{}}, want: ExitNotFound},
		{name: "arquivo sem permissão de execução", args: []string{"run", "--secret-id", "a", "--", "/etc/hosts"},
			fetcher: fakeFetcher{values: map[string]string{}}, want: ExitCannotExecute},
		{name: "variável bloqueada", args: []string{"run", "--secret-id", "a", "--", "app"},
			fetcher: fakeFetcher{values: map[string]string{"NODE_OPTIONS": "--require x"}}, want: ExitFailure},
		{name: "variável liberada com allow-key", args: []string{"run", "--secret-id", "a", "--allow-key", "NODE_OPTIONS", "--", "app"},
			fetcher: fakeFetcher{values: map[string]string{"NODE_OPTIONS": "--max-old-space-size=512"}}, want: ExitOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(nil, tt.fetcher)
			if got := h.app.Run(t.Context(), tt.args); got != tt.want {
				t.Errorf("Run(%v) = %d, esperado %d; stderr: %s", tt.args, got, tt.want, h.stderr)
			}
		})
	}
}

func TestRunExecFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(nil, fakeFetcher{values: map[string]string{}})
	h.app.Exec = func(string, []string, []string) error { return errors.New("permission denied") }
	if got := h.app.Run(t.Context(), []string{"run", "--secret-id", "a", "--", "app"}); got != ExitCannotExecute {
		t.Errorf("Run() = %d, esperado %d", got, ExitCannotExecute)
	}
}

func TestRunTimeoutHint(t *testing.T) {
	t.Parallel()

	h := newHarness(nil, fakeFetcher{err: context.DeadlineExceeded})
	h.app.Run(t.Context(), []string{"run", "--secret-id", "a", "--", "app"})
	if !strings.Contains(h.stderr.String(), "--timeout") {
		t.Errorf("mensagem de timeout deveria sugerir --timeout: %s", h.stderr)
	}
}

func TestRunSignalBeforeExec(t *testing.T) {
	t.Parallel()

	h := newHarness(nil, fakeFetcher{values: map[string]string{}})
	ctx, cancel := context.WithCancel(t.Context())
	released := false
	h.app.ReleaseSignals = func() { released = true; cancel() } // sinal chega no último instante
	if got := h.app.Run(ctx, []string{"run", "--secret-id", "a", "--", "app"}); got != ExitSignaled {
		t.Errorf("Run() = %d, esperado %d", got, ExitSignaled)
	}
	if !released || h.exec != nil {
		t.Errorf("released=%v exec=%v: a aplicação não deveria ser iniciada", released, h.exec)
	}
}

func TestRunSignalDuringFetch(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	h := newHarness(nil, fakeFetcher{err: context.Canceled})
	if got := h.app.Run(ctx, []string{"run", "--secret-id", "a", "--", "app"}); got != ExitSignaled {
		t.Errorf("Run() = %d, esperado %d", got, ExitSignaled)
	}
}

func TestAttemptTimeout(t *testing.T) {
	t.Parallel()

	for total, want := range map[time.Duration]time.Duration{
		time.Second:      time.Second,
		10 * time.Second: 10 * time.Second / 3,
		time.Minute:      5 * time.Second,
	} {
		if got := attemptTimeout(total); got != want {
			t.Errorf("attemptTimeout(%s) = %s, esperado %s", total, got, want)
		}
	}
}
