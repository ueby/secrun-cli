package cli

import (
	"errors"
	"flag"
	"slices"
	"strings"
	"testing"
	"time"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseRunArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		env   map[string]string
		check func(t *testing.T, c *runConfig)
	}{
		{
			name: "flags repetidas e separadas por vírgula",
			args: []string{"--secret-id", "a, b", "--secret-id=c", "--", "app", "--flag-da-app"},
			check: func(t *testing.T, c *runConfig) {
				if !slices.Equal(c.SecretIDs, []string{"a", "b", "c"}) {
					t.Errorf("SecretIDs = %v", c.SecretIDs)
				}
				if !slices.Equal(c.Command, []string{"app", "--flag-da-app"}) {
					t.Errorf("Command = %v", c.Command)
				}
				if !c.Override || c.Timeout != DefaultTimeout {
					t.Errorf("padrões incorretos: override=%v timeout=%v", c.Override, c.Timeout)
				}
			},
		},
		{
			name: "fallback para variáveis de ambiente",
			args: []string{"--", "app"},
			env: map[string]string{
				EnvSecretIDs: "x,y", EnvRegion: "sa-east-1", EnvRoleARN: "arn:role", EnvExternalID: "ext",
				EnvPrefix: "APP_", EnvOverride: "false", EnvTimeout: "3s", EnvVerbose: "1",
			},
			check: func(t *testing.T, c *runConfig) {
				want := runConfig{
					SecretIDs: []string{"x", "y"}, Region: "sa-east-1", RoleARN: "arn:role", ExternalID: "ext",
					Prefix: "APP_", Override: false, Timeout: 3 * time.Second, Verbose: true, Command: []string{"app"},
				}
				if !equalConfig(*c, want) {
					t.Errorf("config = %+v, esperado %+v", *c, want)
				}
			},
		},
		{
			name: "flags têm precedência sobre o ambiente",
			args: []string{"--secret-id", "flag", "--override=true", "--timeout", "1m", "--", "app"},
			env:  map[string]string{EnvSecretIDs: "env", EnvOverride: "false", EnvTimeout: "3s"},
			check: func(t *testing.T, c *runConfig) {
				if !slices.Equal(c.SecretIDs, []string{"flag"}) || !c.Override || c.Timeout != time.Minute {
					t.Errorf("config = %+v", *c)
				}
			},
		},
		{
			name: "flag válida prevalece sobre env inválida",
			args: []string{"--secret-id", "a", "--timeout", "5s", "--override=false", "--", "app"},
			env:  map[string]string{EnvTimeout: "10", EnvOverride: "talvez"},
			check: func(t *testing.T, c *runConfig) {
				if c.Timeout != 5*time.Second || c.Override {
					t.Errorf("config = %+v", *c)
				}
			},
		},
		{
			name: "ids duplicados mantêm a última ocorrência",
			args: []string{"--secret-id", "a,b,a,c,b", "--", "app"},
			check: func(t *testing.T, c *runConfig) {
				if !slices.Equal(c.SecretIDs, []string{"a", "c", "b"}) {
					t.Errorf("SecretIDs = %v", c.SecretIDs)
				}
			},
		},
		{
			name: "allow-key por flag e por env",
			args: []string{"--secret-id", "a", "--", "app"},
			env:  map[string]string{EnvAllowKeys: "NODE_OPTIONS, PATH"},
			check: func(t *testing.T, c *runConfig) {
				if !slices.Equal(c.AllowKeys, []string{"NODE_OPTIONS", "PATH"}) {
					t.Errorf("AllowKeys = %v", c.AllowKeys)
				}
			},
		},
		{
			name: "argumentos após -- não são interpretados",
			args: []string{"--secret-id", "a", "--", "sh", "-c", "echo --", "--secret-id"},
			check: func(t *testing.T, c *runConfig) {
				if !slices.Equal(c.Command, []string{"sh", "-c", "echo --", "--secret-id"}) {
					t.Errorf("Command = %v", c.Command)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := parseRunArgs(tt.args, envFrom(tt.env))
			if err != nil {
				t.Fatalf("parseRunArgs() erro inesperado: %v", err)
			}
			tt.check(t, c)
		})
	}
}

func TestParseRunArgsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantMsg string
	}{
		{name: "sem separador", args: []string{"--secret-id", "a", "app"}, wantMsg: "argumento inesperado"},
		{name: "sem separador nem comando", args: []string{"--secret-id", "a"}, wantMsg: "'--' obrigatório"},
		{name: "sem comando", args: []string{"--secret-id", "a", "--"}, wantMsg: "nenhum comando"},
		{name: "sem segredos", args: []string{"--", "app"}, wantMsg: "--secret-id"},
		{name: "flag desconhecida", args: []string{"--nope", "--", "app"}, wantMsg: "flag desconhecida: -nope"},
		{name: "flag sem valor", args: []string{"--secret-id", "a", "--prefix"}, wantMsg: "requer um valor"},
		{name: "secret-id vazio não cai no env", args: []string{"--secret-id", "", "--", "app"},
			env: map[string]string{EnvSecretIDs: "plain"}, wantMsg: "sem valor"},
		{name: "booleana com espaço", args: []string{"--secret-id", "a", "--override", "false", "--", "app"}, wantMsg: "--override=false"},
		{name: "prefixo reservado", args: []string{"--secret-id", "a", "--prefix", "SECRUN_", "--", "app"}, wantMsg: "reservado"},
		{name: "timeout zero", args: []string{"--secret-id", "a", "--timeout", "0s", "--", "app"}, wantMsg: "timeout"},
		{name: "external-id sem role", args: []string{"--secret-id", "a", "--external-id", "x", "--", "app"}, wantMsg: "--role-arn"},
		{name: "prefixo inválido", args: []string{"--secret-id", "a", "--prefix", "A=", "--", "app"}, wantMsg: "prefixo"},
		{name: "env bool inválida", args: []string{"--secret-id", "a", "--", "app"}, env: map[string]string{EnvOverride: "talvez"}, wantMsg: EnvOverride},
		{name: "env duração inválida", args: []string{"--secret-id", "a", "--", "app"}, env: map[string]string{EnvTimeout: "10"}, wantMsg: EnvTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseRunArgs(tt.args, envFrom(tt.env))
			if err == nil {
				t.Fatal("parseRunArgs() deveria falhar")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("erro %q não contém %q", err, tt.wantMsg)
			}
		})
	}
}

func TestParseRunArgsHelp(t *testing.T) {
	t.Parallel()

	badEnv := envFrom(map[string]string{EnvOverride: "talvez", EnvTimeout: "x"})
	for _, args := range [][]string{{"-h"}, {"--help"}, {"--help", "--", "app"}} {
		if _, err := parseRunArgs(args, badEnv); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("parseRunArgs(%v) = %v, esperado flag.ErrHelp", args, err)
		}
	}
}

func equalConfig(a, b runConfig) bool {
	return slices.Equal(a.SecretIDs, b.SecretIDs) && slices.Equal(a.Command, b.Command) &&
		a.Region == b.Region && a.RoleARN == b.RoleARN && a.ExternalID == b.ExternalID &&
		a.Prefix == b.Prefix && a.Override == b.Override && a.Timeout == b.Timeout && a.Verbose == b.Verbose
}
