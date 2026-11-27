package awsconfig

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
)

func TestLoadRegionAndRetry(t *testing.T) {
	// Isola o teste de perfis/credenciais da máquina local.
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")

	cfg, err := Load(t.Context(), Options{Region: "sa-east-1"})
	if err != nil {
		t.Fatalf("Load() erro inesperado: %v", err)
	}
	if cfg.Region != "sa-east-1" {
		t.Errorf("Region = %q", cfg.Region)
	}
	if got := cfg.Retryer().MaxAttempts(); got != DefaultMaxAttempts {
		t.Errorf("MaxAttempts = %d, esperado %d", got, DefaultMaxAttempts)
	}
}

func TestLoadAssumeRole(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")

	cfg, err := Load(t.Context(), Options{
		Region:      "us-east-1",
		RoleARN:     "arn:aws:iam::123456789012:role/secrun-reader",
		ExternalID:  "ext",
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("Load() erro inesperado: %v", err)
	}
	if !aws.IsCredentialsProvider(cfg.Credentials, (*stscreds.AssumeRoleProvider)(nil)) {
		t.Error("credenciais deveriam usar AssumeRoleProvider")
	}
	if got := cfg.Retryer().MaxAttempts(); got != 3 {
		t.Errorf("MaxAttempts = %d, esperado 3", got)
	}
}

func TestSessionName(t *testing.T) {
	t.Parallel()

	name := sessionName()
	if len(name) < 2 || len(name) > 64 {
		t.Errorf("tamanho inválido: %q", name)
	}
	if !regexp.MustCompile(`^secrun[\w+=,.@-]*$`).MatchString(name) {
		t.Errorf("RoleSessionName inválido: %q", name)
	}
}

// isolateAWSEnv remove do ambiente do teste toda fonte de região/credenciais
// da máquina local, para que só o IMDS emulado possa informar a região.
func isolateAWSEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_PROFILE", "AWS_EC2_METADATA_DISABLED"} {
		t.Setenv(k, "")
	}
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
}

// fakeIMDS emula o IMDSv2: token via PUT e identity document via GET. Com hang, nunca
// responde (como fora da EC2, em que o endereço não existe).
func fakeIMDS(t *testing.T, region string, hang bool) *httptest.Server {
	t.Helper()
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hang {
			select {
			case <-r.Context().Done():
			case <-stop:
			}
			return
		}
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/latest/api/token":
			w.Header().Set("X-Aws-Ec2-Metadata-Token-Ttl-Seconds", "21600")
			_, _ = w.Write([]byte("token-de-teste"))
		case r.Method == http.MethodGet && r.URL.Path == "/latest/dynamic/instance-identity/document":
			// GetRegion do SDK lê o campo "region" do instance identity document.
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"region":%q,"instanceId":"i-0123456789abcdef0"}`, region)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { close(stop); srv.CloseClientConnections(); srv.Close() })
	return srv
}

func TestLoadRegionFromIMDS(t *testing.T) {
	isolateAWSEnv(t)
	t.Setenv("AWS_EC2_METADATA_SERVICE_ENDPOINT", fakeIMDS(t, "sa-east-1", false).URL)

	cfg, err := Load(t.Context(), Options{})
	if err != nil {
		t.Fatalf("Load() erro inesperado: %v", err)
	}
	if cfg.Region != "sa-east-1" {
		t.Errorf("Region = %q, esperado sa-east-1 (via IMDS)", cfg.Region)
	}
}

func TestLoadRegionIMDSHangsFailsFast(t *testing.T) {
	isolateAWSEnv(t)
	t.Setenv("AWS_EC2_METADATA_SERVICE_ENDPOINT", fakeIMDS(t, "", true).URL)

	// Prazo total generoso: a falha deve vir do limite próprio do IMDS.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()
	_, err := Load(ctx, Options{})
	elapsed := time.Since(start)

	if !errors.Is(err, ErrRegionNotSet) {
		t.Fatalf("erro = %v, esperado ErrRegionNotSet", err)
	}
	if elapsed > IMDSRegionTimeout+time.Second {
		t.Errorf("levou %s; deveria falhar em ~%s", elapsed, IMDSRegionTimeout)
	}
}

func TestLoadRegionIMDSDisabled(t *testing.T) {
	isolateAWSEnv(t)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	start := time.Now()
	_, err := Load(t.Context(), Options{})
	if !errors.Is(err, ErrRegionNotSet) {
		t.Fatalf("erro = %v, esperado ErrRegionNotSet", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("com IMDS desativado a falha deveria ser imediata (levou %s)", elapsed)
	}
	if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "ec2imds") {
		t.Errorf("mensagem deveria orientar sobre a região, não sobre IMDS/timeout: %v", err)
	}
}

func TestLoadExplicitRegionSkipsIMDS(t *testing.T) {
	isolateAWSEnv(t)
	t.Setenv("AWS_EC2_METADATA_SERVICE_ENDPOINT", fakeIMDS(t, "", true).URL)

	start := time.Now()
	cfg, err := Load(t.Context(), Options{Region: "us-west-2"})
	if err != nil || cfg.Region != "us-west-2" {
		t.Fatalf("Load() = %q, %v", cfg.Region, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("com região informada o IMDS não deveria ser consultado (levou %s)", elapsed)
	}
}
