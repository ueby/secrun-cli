// Package cli implementa a interface de linha de comando do secrun.
//
// Toda dependência de sistema (ambiente, busca de executáveis, exec e acesso
// à AWS) é injetável via App, permitindo testar o fluxo completo sem
// substituir o processo de teste nem acessar a AWS.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"github.com/ueby/secrun-cli/internal/awsconfig"
	"github.com/ueby/secrun-cli/internal/environ"
	"github.com/ueby/secrun-cli/internal/secrets"
	"github.com/ueby/secrun-cli/internal/version"
)

// Códigos de saída. 126 e 127 seguem a convenção de shells POSIX.
const (
	ExitOK            = 0   // sucesso (ou ajuda/versão)
	ExitFailure       = 1   // falha ao obter/processar segredos
	ExitUsage         = 2   // uso incorreto da CLI
	ExitCannotExecute = 126 // comando encontrado, mas não pôde ser executado
	ExitNotFound      = 127 // comando não encontrado
	ExitSignaled      = 143 // interrompido por sinal antes de iniciar a aplicação (128+SIGTERM)
)

// SecretFetcher obtém e mescla segredos a partir de seus identificadores.
type SecretFetcher interface {
	FetchAll(ctx context.Context, ids []string) (map[string]string, error)
}

// App reúne as dependências externas da CLI.
type App struct {
	Stdout   io.Writer
	Stderr   io.Writer
	Getenv   func(string) string
	Environ  func() []string
	LookPath func(file string) (string, error)
	// Exec substitui o processo atual (em produção, syscall.Exec). Em caso
	// de sucesso não retorna.
	Exec func(argv0 string, argv, envv []string) error
	// NewFetcher cria o SecretFetcher a partir das opções AWS.
	NewFetcher func(ctx context.Context, opts awsconfig.Options) (SecretFetcher, error)
	// ReleaseSignals, se definido, é chamado imediatamente antes do Exec para
	// devolver aos sinais o comportamento padrão. Sem isso, um SIGTERM
	// recebido nesse intervalo seria capturado e descartado.
	ReleaseSignals func()
}

// New retorna um App configurado com as implementações reais.
func New() *App {
	return &App{
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Getenv:     os.Getenv,
		Environ:    os.Environ,
		LookPath:   exec.LookPath,
		Exec:       syscall.Exec,
		NewFetcher: newAWSFetcher,
	}
}

func newAWSFetcher(ctx context.Context, opts awsconfig.Options) (SecretFetcher, error) {
	cfg, err := awsconfig.Load(ctx, opts)
	if err != nil {
		return nil, err
	}
	return secrets.NewFetcher(secretsmanager.NewFromConfig(cfg)), nil
}

// Run executa a CLI com os argumentos informados (sem o nome do programa)
// e retorna o código de saída.
func (a *App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.Stderr, usage)
		return ExitUsage
	}

	switch args[0] {
	case "run":
		return a.run(ctx, args[1:])
	case "version", "--version", "-version":
		fmt.Fprintln(a.Stdout, version.Get())
		return ExitOK
	case "help", "--help", "-help", "-h":
		fmt.Fprint(a.Stdout, usage)
		return ExitOK
	default:
		a.errorf("comando desconhecido %q", args[0])
		fmt.Fprint(a.Stderr, "\n"+usage)
		return ExitUsage
	}
}

func (a *App) run(ctx context.Context, args []string) int {
	cfg, err := parseRunArgs(args, a.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(a.Stdout, runUsage)
		return ExitOK
	}
	if err != nil {
		a.errorf("%v", err)
		fmt.Fprintln(a.Stderr, "Use 'secrun run --help' para ver as opções.")
		return ExitUsage
	}

	log := a.logger(cfg.Verbose)

	signalCtx := ctx // cancelado pelo main ao receber SIGINT/SIGTERM
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	fetcher, err := a.NewFetcher(ctx, awsconfig.Options{
		Region:         cfg.Region,
		RoleARN:        cfg.RoleARN,
		ExternalID:     cfg.ExternalID,
		AttemptTimeout: attemptTimeout(cfg.Timeout),
	})
	if err != nil {
		return a.fail(signalCtx, withTimeoutHint(err, cfg))
	}

	log.Debug("buscando segredos", "secret_ids", cfg.SecretIDs, "role_arn", cfg.RoleARN, "timeout", cfg.Timeout)
	values, err := fetcher.FetchAll(ctx, cfg.SecretIDs)
	if err != nil {
		return a.fail(signalCtx, withTimeoutHint(err, cfg))
	}

	env, err := environ.Build(a.Environ(), values, environ.Options{
		Prefix:    cfg.Prefix,
		Override:  cfg.Override,
		AllowKeys: cfg.AllowKeys,
	})
	if err != nil {
		a.errorf("%v", err)
		return ExitFailure
	}

	// O binário é resolvido com o PATH do próprio secrun (o do container),
	// nunca com um PATH vindo de segredo.
	binary, err := a.LookPath(cfg.Command[0])
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			a.errorf("executável não encontrado %q: %v", cfg.Command[0], err)
			return ExitNotFound
		}
		a.errorf("não é possível executar %q: %v", cfg.Command[0], err)
		return ExitCannotExecute
	}

	// Somente a quantidade é registrada: nomes e valores não vão para logs.
	log.Debug("iniciando aplicação", "command", binary, "injected_vars", len(values))

	// Libera o contexto e devolve os sinais ao comportamento padrão. Se um
	// sinal chegou antes disso, a aplicação não deve ser iniciada.
	cancel()
	if a.ReleaseSignals != nil {
		a.ReleaseSignals()
	}
	if signalCtx.Err() != nil {
		a.errorf("interrompido por sinal antes de iniciar a aplicação")
		return ExitSignaled
	}
	if err := a.Exec(binary, cfg.Command, env); err != nil {
		a.errorf("falha ao executar %q: %v", binary, err)
		return ExitCannotExecute
	}
	return ExitOK // alcançado apenas quando Exec é um dublê de teste
}

// fail reporta err e retorna o código de saída adequado: ExitSignaled se a
// execução foi interrompida por sinal, ExitFailure caso contrário.
func (a *App) fail(signalCtx context.Context, err error) int {
	if signalCtx.Err() != nil {
		a.errorf("interrompido por sinal antes de iniciar a aplicação")
		return ExitSignaled
	}
	a.errorf("%v", err)
	return ExitFailure
}

// attemptTimeout limita cada tentativa HTTP, para que uma conexão travada
// não consuma todo o --timeout e o retry tenha chance de agir.
func attemptTimeout(total time.Duration) time.Duration {
	return min(max(total/3, time.Second), 5*time.Second)
}

func (a *App) errorf(format string, args ...any) {
	fmt.Fprintf(a.Stderr, "secrun: erro: "+format+"\n", args...)
}

func (a *App) logger(verbose bool) *slog.Logger {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(a.Stderr, &slog.HandlerOptions{Level: level})).
		With("component", "secrun")
}

func withTimeoutHint(err error, cfg *runConfig) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w (tempo limite de %s excedido; ajuste com --timeout ou %s)", err, cfg.Timeout, EnvTimeout)
	}
	return err
}
