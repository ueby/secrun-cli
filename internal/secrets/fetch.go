// Package secrets busca segredos no AWS Secrets Manager e os converte em
// pares chave/valor prontos para serem injetados como variáveis de ambiente.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// ErrBinarySecret indica que o segredo foi gravado como SecretBinary, formato
// não suportado pelo secrun.
var ErrBinarySecret = errors.New("segredo não possui SecretString (segredos binários não são suportados)")

// Client é o subconjunto da API do AWS Secrets Manager utilizado pelo secrun.
// É satisfeito por *secretsmanager.Client e facilita o uso de dublês em testes.
type Client interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput,
		optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// SecretError associa uma falha ao identificador do segredo que a causou.
// O identificador (nome ou ARN) não é considerado sensível.
type SecretError struct {
	SecretID string
	Err      error
}

func (e *SecretError) Error() string {
	return fmt.Sprintf("segredo %q: %v", e.SecretID, e.Err)
}

func (e *SecretError) Unwrap() error { return e.Err }

// Fetcher busca e mescla múltiplos segredos.
type Fetcher struct {
	client Client
}

// NewFetcher cria um Fetcher que usa o client informado.
func NewFetcher(client Client) *Fetcher {
	return &Fetcher{client: client}
}

// FetchAll busca todos os segredos em paralelo e os mescla respeitando a
// ordem de ids: chaves de segredos posteriores sobrescrevem as anteriores.
//
// Na primeira falha, as demais requisições em andamento são canceladas e o
// erro mais relevante (o primeiro, na ordem de ids, que não seja um
// cancelamento provocado pelo próprio FetchAll) é retornado.
func (f *Fetcher) FetchAll(ctx context.Context, ids []string) (map[string]string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]map[string]string, len(ids))
	errs := make([]error, len(ids))

	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() {
			values, err := f.fetchOne(ctx, id)
			if err != nil {
				errs[i] = err
				cancel()
				return
			}
			results[i] = values
		})
	}
	wg.Wait()

	if err := firstRelevantError(errs); err != nil {
		return nil, err
	}

	merged := make(map[string]string)
	for _, values := range results {
		for k, v := range values {
			merged[k] = v
		}
	}
	return merged, nil
}

func (f *Fetcher) fetchOne(ctx context.Context, id string) (map[string]string, error) {
	out, err := f.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(id),
	})
	if err != nil {
		return nil, &SecretError{SecretID: id, Err: err}
	}
	if out.SecretString == nil {
		return nil, &SecretError{SecretID: id, Err: ErrBinarySecret}
	}

	values, err := Parse(*out.SecretString)
	if err != nil {
		return nil, &SecretError{SecretID: id, Err: err}
	}
	return values, nil
}

func firstRelevantError(errs []error) error {
	var first error
	for _, err := range errs {
		if err == nil {
			continue
		}
		if !errors.Is(err, context.Canceled) {
			return err
		}
		if first == nil {
			first = err
		}
	}
	return first
}
