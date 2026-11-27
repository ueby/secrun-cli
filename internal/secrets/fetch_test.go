package secrets

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// fakeClient simula o Secrets Manager a partir de um mapa id -> SecretString.
// Um valor nil simula um segredo binário; ids presentes em errs falham.
type fakeClient struct {
	values map[string]*string
	errs   map[string]error
}

func (f *fakeClient) GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput,
	_ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	id := aws.ToString(in.SecretId)
	if err, ok := f.errs[id]; ok {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v, ok := f.values[id]
	if !ok {
		return nil, errors.New("ResourceNotFoundException")
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: v}, nil
}

func TestFetchAllMergesInOrder(t *testing.T) {
	t.Parallel()

	client := &fakeClient{values: map[string]*string{
		"shared": aws.String(`{"DB_HOST":"shared-db","LOG_LEVEL":"info"}`),
		"app":    aws.String(`{"DB_HOST":"app-db","API_KEY":"k"}`),
	}}

	got, err := NewFetcher(client).FetchAll(t.Context(), []string{"shared", "app"})
	if err != nil {
		t.Fatalf("FetchAll() erro inesperado: %v", err)
	}
	want := map[string]string{"DB_HOST": "app-db", "LOG_LEVEL": "info", "API_KEY": "k"}
	if !maps.Equal(got, want) {
		t.Errorf("FetchAll() = %v, esperado %v", got, want)
	}

	// Ordem inversa: o segredo "shared" passa a prevalecer.
	got, err = NewFetcher(client).FetchAll(t.Context(), []string{"app", "shared"})
	if err != nil {
		t.Fatalf("FetchAll() erro inesperado: %v", err)
	}
	if got["DB_HOST"] != "shared-db" {
		t.Errorf("DB_HOST = %q, esperado shared-db", got["DB_HOST"])
	}
}

func TestFetchAllErrors(t *testing.T) {
	t.Parallel()

	awsErr := errors.New("AccessDeniedException")
	client := &fakeClient{
		values: map[string]*string{
			"ok":     aws.String(`{"K":"v"}`),
			"binary": nil,
			"bad":    aws.String(`{"K": SUPERSECRETO}`),
		},
		errs: map[string]error{"denied": awsErr},
	}

	tests := []struct {
		name   string
		ids    []string
		target error
		id     string
	}{
		{name: "erro AWS", ids: []string{"ok", "denied"}, target: awsErr, id: "denied"},
		{name: "segredo binário", ids: []string{"binary"}, target: ErrBinarySecret, id: "binary"},
		{name: "JSON inválido", ids: []string{"ok", "bad"}, target: ErrInvalidJSON, id: "bad"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewFetcher(client).FetchAll(t.Context(), tt.ids)
			if !errors.Is(err, tt.target) {
				t.Fatalf("erro = %v, esperado %v", err, tt.target)
			}
			var se *SecretError
			if !errors.As(err, &se) || se.SecretID != tt.id {
				t.Errorf("erro deveria identificar o segredo %q: %v", tt.id, err)
			}
			if strings.Contains(err.Error(), "SUPERSECRETO") {
				t.Errorf("mensagem de erro vaza conteúdo do segredo: %q", err)
			}
		})
	}
}

func TestFirstRelevantErrorPrefersRootCause(t *testing.T) {
	t.Parallel()

	root := errors.New("AccessDeniedException")
	errs := []error{nil, &SecretError{"a", context.Canceled}, &SecretError{"b", root}}
	if got := firstRelevantError(errs); !errors.Is(got, root) {
		t.Errorf("firstRelevantError() = %v, esperado a causa raiz", got)
	}
	if got := firstRelevantError([]error{nil, nil}); got != nil {
		t.Errorf("firstRelevantError() = %v, esperado nil", got)
	}
}
