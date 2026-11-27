// Package awsconfig carrega a configuração do AWS SDK usada pelo secrun.
//
// As credenciais são resolvidas pela cadeia padrão do SDK, o que cobre
// nativamente ECS Task Role, EKS IRSA (web identity), EKS Pod Identity,
// perfil de instância EC2 e perfis locais (~/.aws). Nenhuma credencial
// estática é exigida.
package awsconfig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Valores padrão de resiliência, dimensionados para suportar picos de
// inicialização simultânea de containers (auto scaling).
const (
	DefaultMaxAttempts    = 5
	DefaultMaxBackoff     = 2 * time.Second
	DefaultAttemptTimeout = 3 * time.Second
)

// Options define como a configuração AWS é montada.
type Options struct {
	// Region sobrescreve a região resolvida pelo SDK (AWS_REGION,
	// AWS_DEFAULT_REGION, perfil ou IMDS). Opcional.
	Region string
	// RoleARN, quando informado, faz o secrun assumir esta IAM Role via STS
	// antes de acessar o Secrets Manager (cenários cross-account).
	RoleARN string
	// ExternalID é enviado no AssumeRole quando a trust policy o exige.
	ExternalID string
	// MaxAttempts é o número máximo de tentativas por chamada à AWS.
	MaxAttempts int
	// MaxBackoff é o intervalo máximo entre tentativas.
	MaxBackoff time.Duration
	// AttemptTimeout limita cada tentativa HTTP (conexão + resposta). Sem
	// ele, uma conexão que nunca responde consumiria todo o prazo total e
	// nenhuma nova tentativa seria feita.
	AttemptTimeout time.Duration
}

// Load monta um aws.Config com retry exponencial e, opcionalmente,
// credenciais obtidas via AssumeRole.
func Load(ctx context.Context, opts Options) (aws.Config, error) {
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	maxBackoff := opts.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = DefaultMaxBackoff
	}
	attemptTimeout := opts.AttemptTimeout
	if attemptTimeout <= 0 {
		attemptTimeout = DefaultAttemptTimeout
	}

	loadOpts := []func(*config.LoadOptions) error{
		config.WithRetryer(func() aws.Retryer {
			return retry.NewStandard(func(o *retry.StandardOptions) {
				o.MaxAttempts = maxAttempts
				o.MaxBackoff = maxBackoff
			})
		}),
		config.WithHTTPClient(awshttp.NewBuildableClient().WithTimeout(attemptTimeout)),
	}
	if opts.Region != "" {
		loadOpts = append(loadOpts, config.WithRegion(opts.Region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("falha ao carregar configuração AWS: %w", err)
	}
	if cfg.Region == "" {
		// Último recurso (ex.: EC2/EKS sem AWS_REGION). ECS, IRSA e Pod
		// Identity já definem AWS_REGION automaticamente.
		region, err := regionFromIMDS(ctx, cfg)
		if err != nil {
			return aws.Config{}, ErrRegionNotSet
		}
		cfg.Region = region
	}

	if opts.RoleARN != "" {
		provider := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), opts.RoleARN,
			func(o *stscreds.AssumeRoleOptions) {
				o.RoleSessionName = sessionName()
				if opts.ExternalID != "" {
					o.ExternalID = aws.String(opts.ExternalID)
				}
			})
		cfg.Credentials = aws.NewCredentialsCache(provider)
	}

	return cfg, nil
}

// ErrRegionNotSet indica que nenhuma fonte informou a região AWS e que o
// metadata da instância EC2 (IMDS) não respondeu ou está desativado.
var ErrRegionNotSet = errors.New("região AWS não definida: use --region, SECRUN_REGION ou AWS_REGION")

// IMDSRegionTimeout limita a consulta da região ao IMDS. Fora da EC2 o IMDS
// não responde; sem este limite, o cliente padrão consumiria todo o --timeout
// em novas tentativas antes de falhar (issue #8).
const IMDSRegionTimeout = 2 * time.Second

// regionFromIMDS consulta a região no IMDS com prazo e tentativas próprios.
// Respeita AWS_EC2_METADATA_DISABLED e AWS_EC2_METADATA_SERVICE_ENDPOINT.
//
// Diferente de config.WithEC2IMDSRegion, a falha aqui não é fatal por si: o
// chamador a converte em ErrRegionNotSet, com orientação clara ao usuário.
func regionFromIMDS(ctx context.Context, cfg aws.Config) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, IMDSRegionTimeout)
	defer cancel()

	client := imds.NewFromConfig(cfg, func(o *imds.Options) {
		o.Retryer = retry.AddWithMaxAttempts(retry.NewStandard(), 2)
		o.HTTPClient = awshttp.NewBuildableClient().WithTimeout(time.Second)
	})
	out, err := client.GetRegion(ctx, &imds.GetRegionInput{})
	if err != nil {
		return "", err
	}
	if out.Region == "" {
		return "", errors.New("IMDS retornou região vazia")
	}
	return out.Region, nil
}

// invalidSessionChars casa caracteres não aceitos em RoleSessionName.
var invalidSessionChars = regexp.MustCompile(`[^\w+=,.@-]`)

// sessionName gera um RoleSessionName rastreável no CloudTrail. Em ECS/EKS o
// hostname corresponde ao ID da task/pod, o que facilita a auditoria.
func sessionName() string {
	name := "secrun"
	if host, err := os.Hostname(); err == nil && host != "" {
		name += "-" + invalidSessionChars.ReplaceAllString(host, "-")
	}
	const maxLen = 64 // limite imposto pelo STS
	if len(name) > maxLen {
		name = name[:maxLen]
	}
	return name
}
