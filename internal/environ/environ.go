// Package environ monta o ambiente final entregue ao processo da aplicação,
// mesclando as variáveis atuais do container com os segredos obtidos.
package environ

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ueby/secrun-cli/internal/secrets"
)

// InternalPrefix é o prefixo das variáveis de configuração do próprio secrun.
// Todas as variáveis com este prefixo são removidas antes de iniciar o
// processo filho, e segredos não podem definir variáveis com ele.
const InternalPrefix = "SECRUN_"

// ErrDeniedKey indica que um segredo tentou definir uma variável bloqueada
// pela política de segurança (ver IsDenied).
var ErrDeniedKey = errors.New("variável bloqueada por segurança")

// Options controla como os segredos são mesclados ao ambiente.
type Options struct {
	// Prefix é adicionado ao nome de cada variável vinda dos segredos
	// (ex.: "APP_" transforma DB_PASSWORD em APP_DB_PASSWORD).
	Prefix string
	// Override, quando verdadeiro, faz os segredos sobrescreverem variáveis
	// já existentes no ambiente. Quando falso, valores existentes prevalecem.
	Override bool
	// AllowKeys libera nomes (já com Prefix) que seriam bloqueados por
	// IsDenied. A comparação é exata.
	AllowKeys []string
}

// Build retorna o ambiente final no formato "CHAVE=valor", ordenado por
// chave para garantir saída determinística.
//
// Etapas:
//  1. carrega current (entradas sem '=' são descartadas; em nomes
//     duplicados vale a primeira ocorrência, como em getenv(3));
//  2. remove variáveis com InternalPrefix;
//  3. valida e aplica os segredos (com Prefix), respeitando Override.
//
// Um segredo que defina variável com InternalPrefix, ou bloqueada por
// IsDenied e ausente de AllowKeys, interrompe a montagem com erro.
func Build(current []string, secretValues map[string]string, opts Options) ([]string, error) {
	env := make(map[string]string, len(current)+len(secretValues))

	for _, entry := range current {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" || strings.HasPrefix(key, InternalPrefix) {
			continue
		}
		if _, dup := env[key]; dup {
			continue
		}
		env[key] = value
	}

	// Ordem determinística, para que o erro reportado seja sempre o mesmo.
	secretKeys := make([]string, 0, len(secretValues))
	for k := range secretValues {
		secretKeys = append(secretKeys, k)
	}
	slices.Sort(secretKeys)

	for _, key := range secretKeys {
		name := opts.Prefix + key
		if err := checkName(name, opts.AllowKeys); err != nil {
			return nil, err
		}
		if _, exists := env[name]; exists && !opts.Override {
			continue
		}
		env[name] = secretValues[key]
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out, nil
}

func checkName(name string, allow []string) error {
	if err := secrets.ValidateKey(name); err != nil {
		return err
	}
	if strings.HasPrefix(strings.ToUpper(name), InternalPrefix) {
		return fmt.Errorf("%w: %q usa o prefixo reservado %s", ErrDeniedKey, name, InternalPrefix)
	}
	if IsDenied(name) && !slices.Contains(allow, name) {
		return fmt.Errorf("%w: %q pode alterar a execução, a identidade AWS ou o tráfego da aplicação; "+
			"use --allow-key %s se for intencional", ErrDeniedKey, name, name)
	}
	return nil
}

// ValidatePrefix verifica se prefix pode compor nomes de variáveis.
func ValidatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if err := secrets.ValidateKey(prefix); err != nil {
		return fmt.Errorf("prefixo inválido: %w", err)
	}
	if strings.HasPrefix(strings.ToUpper(prefix), InternalPrefix) {
		return fmt.Errorf("prefixo inválido: %s é reservado", InternalPrefix)
	}
	return nil
}
