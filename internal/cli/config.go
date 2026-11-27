package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ueby/secrun-cli/internal/environ"
)

// DefaultTimeout é o tempo máximo padrão para obter todos os segredos.
const DefaultTimeout = 10 * time.Second

// Variáveis de ambiente reconhecidas pelo subcomando run. Todas são removidas
// do ambiente antes de iniciar a aplicação (ver environ.InternalPrefix).
const (
	EnvSecretIDs  = "SECRUN_IDS" //nolint:gosec // G101: nome de variável, não uma credencial
	EnvRegion     = "SECRUN_REGION"
	EnvRoleARN    = "SECRUN_ROLE_ARN"
	EnvExternalID = "SECRUN_EXTERNAL_ID"
	EnvPrefix     = "SECRUN_PREFIX"
	EnvOverride   = "SECRUN_OVERRIDE"
	EnvTimeout    = "SECRUN_TIMEOUT"
	EnvVerbose    = "SECRUN_VERBOSE"
	EnvAllowKeys  = "SECRUN_ALLOW_KEYS"
)

// runConfig é a configuração resolvida do subcomando run.
// Precedência: flag > variável de ambiente > valor padrão.
type runConfig struct {
	SecretIDs  []string
	Region     string
	RoleARN    string
	ExternalID string
	Prefix     string
	AllowKeys  []string
	Override   bool
	Timeout    time.Duration
	Verbose    bool
	Command    []string
}

// stringList implementa flag.Value aceitando a flag repetida e/ou valores
// separados por vírgula.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(value string) error {
	*s = append(*s, splitList(value)...)
	return nil
}

func splitList(value string) []string {
	var out []string
	for item := range strings.SplitSeq(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// parseRunArgs interpreta os argumentos de "secrun run".
// Retorna flag.ErrHelp quando -h/--help é solicitado.
//
// As flags são interpretadas antes das variáveis de ambiente: uma variável
// SECRUN_* só é lida (e validada) quando a flag correspondente não foi
// informada. Assim, um valor inválido no ambiente não impede o uso da flag
// nem do --help.
func parseRunArgs(args []string, getenv func(string) string) (*runConfig, error) {
	flagArgs, command, hasSeparator := splitAtSeparator(args)

	cfg := &runConfig{Override: true, Timeout: DefaultTimeout}
	var ids, allow stringList

	fs := flag.NewFlagSet("secrun run", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // mensagens de erro e ajuda são tratadas pelo chamador
	fs.Var(&ids, "secret-id", "")
	fs.StringVar(&cfg.Region, "region", "", "")
	fs.StringVar(&cfg.RoleARN, "role-arn", "", "")
	fs.StringVar(&cfg.ExternalID, "external-id", "", "")
	fs.StringVar(&cfg.Prefix, "prefix", "", "")
	fs.Var(&allow, "allow-key", "")
	fs.BoolVar(&cfg.Override, "override", cfg.Override, "")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "")

	if err := fs.Parse(flagArgs); err != nil {
		return nil, translateFlagError(err)
	}
	if fs.NArg() > 0 {
		arg := fs.Arg(0)
		if arg == "true" || arg == "false" {
			return nil, fmt.Errorf("argumento inesperado %q: em flags booleanas use a forma --flag=%s (ex.: --override=%s)", arg, arg, arg)
		}
		return nil, fmt.Errorf("argumento inesperado %q: o comando da aplicação deve vir após '--'", arg)
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if set["secret-id"] {
		if len(ids) == 0 {
			return nil, errors.New("--secret-id informado sem valor")
		}
		cfg.SecretIDs = ids
	} else {
		cfg.SecretIDs = splitList(getenv(EnvSecretIDs))
	}
	cfg.SecretIDs = dedupeKeepLast(cfg.SecretIDs)

	if set["allow-key"] {
		cfg.AllowKeys = allow
	} else {
		cfg.AllowKeys = splitList(getenv(EnvAllowKeys))
	}

	envString := func(name, env string, dst *string) {
		if !set[name] {
			*dst = getenv(env)
		}
	}
	envString("region", EnvRegion, &cfg.Region)
	envString("role-arn", EnvRoleARN, &cfg.RoleARN)
	envString("external-id", EnvExternalID, &cfg.ExternalID)
	envString("prefix", EnvPrefix, &cfg.Prefix)

	if err := envBool(set["override"], EnvOverride, getenv, &cfg.Override); err != nil {
		return nil, err
	}
	if err := envBool(set["verbose"], EnvVerbose, getenv, &cfg.Verbose); err != nil {
		return nil, err
	}
	if v := getenv(EnvTimeout); v != "" && !set["timeout"] {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("%s inválida %q: use uma duração como 10s ou 1m", EnvTimeout, v)
		}
		cfg.Timeout = d
	}

	if !hasSeparator {
		return nil, errors.New("separador '--' obrigatório antes do comando da aplicação (ex.: secrun run --secret-id app/prod -- ./app)")
	}
	cfg.Command = command

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func envBool(flagSet bool, env string, getenv func(string) string, dst *bool) error {
	v := getenv(env)
	if flagSet || v == "" {
		return nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fmt.Errorf("%s inválida %q: use true ou false", env, v)
	}
	*dst = b
	return nil
}

func (c *runConfig) validate() error {
	switch {
	case len(c.SecretIDs) == 0:
		return fmt.Errorf("informe ao menos um --secret-id ou defina a variável %s", EnvSecretIDs)
	case len(c.Command) == 0:
		return errors.New("nenhum comando especificado após '--'")
	case c.Timeout <= 0:
		return fmt.Errorf("--timeout deve ser positivo (recebido %s)", c.Timeout)
	case c.ExternalID != "" && c.RoleARN == "":
		return errors.New("--external-id requer --role-arn")
	}
	return environ.ValidatePrefix(c.Prefix)
}

// dedupeKeepLast remove ids repetidos mantendo a ÚLTIMA ocorrência, que é a
// que define a precedência na mesclagem ("a,b,a" equivale a "b,a").
func dedupeKeepLast(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		if !seen[ids[i]] {
			seen[ids[i]] = true
			out = append(out, ids[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// translateFlagError traduz as mensagens do pacote flag para português.
func translateFlagError(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return err
	}
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "flag provided but not defined: "):
		return fmt.Errorf("flag desconhecida: %s", strings.TrimPrefix(msg, "flag provided but not defined: "))
	case strings.HasPrefix(msg, "flag needs an argument: "):
		return fmt.Errorf("a flag %s requer um valor", strings.TrimPrefix(msg, "flag needs an argument: "))
	case strings.HasPrefix(msg, "invalid "):
		return fmt.Errorf("valor de flag inválido: %s", msg)
	default:
		return err
	}
}

// splitAtSeparator separa os argumentos no primeiro "--".
func splitAtSeparator(args []string) (flags, command []string, found bool) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:], true
		}
	}
	return args, nil, false
}
