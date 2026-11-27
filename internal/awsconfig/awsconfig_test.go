package awsconfig

import (
	"regexp"
	"testing"

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
