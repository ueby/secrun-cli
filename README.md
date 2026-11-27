# secrun

[![CI](https://github.com/ueby/secrun-cli/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ueby/secrun-cli/actions/workflows/ci.yml)
[![CodeQL](https://github.com/ueby/secrun-cli/actions/workflows/codeql.yml/badge.svg?branch=main)](https://github.com/ueby/secrun-cli/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/ueby/secrun-cli/badge)](https://scorecard.dev/viewer/?uri=github.com/ueby/secrun-cli)
[![Release](https://img.shields.io/github/v/release/ueby/secrun-cli?sort=semver&display_name=tag)](https://github.com/ueby/secrun-cli/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/ueby/secrun-cli.svg)](https://pkg.go.dev/github.com/ueby/secrun-cli/cmd/secrun)
[![Go Report Card](https://goreportcard.com/badge/github.com/ueby/secrun-cli)](https://goreportcard.com/report/github.com/ueby/secrun-cli)
[![Go version](https://img.shields.io/github/go-mod/go-version/ueby/secrun-cli)](go.mod)
[![License: MIT](https://img.shields.io/github/license/ueby/secrun-cli)](LICENSE)

**Injetor de segredos em tempo de execução para containers no Amazon ECS e Amazon EKS.**

O `secrun` roda como `ENTRYPOINT` do container: busca um ou mais segredos JSON no
**AWS Secrets Manager** usando a identidade IAM nativa do workload, injeta cada chave
como variável de ambiente e **substitui a si mesmo** pelo processo da aplicação
(`exec`), preservando o PID 1 e a entrega de sinais.

```text
┌──────────── container ────────────┐
│ PID 1: secrun run -- node app.js  │──── GetSecretValue ───▶ AWS Secrets Manager
│        │  (ECS Task Role / IRSA / │                          secrun/<projeto>/<ambiente>
│        │   EKS Pod Identity)      │
│        ▼ exec (mesmo PID)         │
│ PID 1: node app.js  + env vars    │◀── SIGTERM do orquestrador
└───────────────────────────────────┘
```

## Garantias

| Garantia | Como |
|---|---|
| **Sem "secret zero"** | Credenciais resolvidas pela cadeia padrão do AWS SDK (Task Role, IRSA, Pod Identity). Nenhuma chave estática na imagem ou na task. |
| **Sem persistência em disco** | Os segredos ficam apenas em memória e no ambiente do processo da aplicação. |
| **PID 1 preservado** | `syscall.Exec` substitui o processo; `SIGTERM` chega direto na aplicação. |
| **Resiliência no auto scaling** | Retry exponencial (até 5 tentativas, backoff máx. 2s) com timeout por tentativa, busca paralela e prazo total rígido (padrão 10s). |
| **Logs seguros** | Valores e trechos de `SecretString` nunca aparecem no `stderr`, nem mesmo em erros de parse. |
| **Escrever no segredo ≠ executar código** | Segredos não podem definir variáveis como `LD_PRELOAD`, `NODE_OPTIONS`, `PATH` ou `HTTPS_PROXY` sem liberação explícita (`--allow-key`). Veja [SECURITY.md](SECURITY.md#variáveis-bloqueadas). |
| **Sem vazamento de configuração** | Todas as variáveis `SECRUN_*` são removidas antes de iniciar a aplicação e não podem ser definidas por segredos. |
| **Supply chain verificável** | Releases com SHA-256 e proveniência SLSA (`gh attestation verify`); imagem com SBOM. |

## Instalação

```bash
V=v0.1.0
curl -fsSL "https://github.com/ueby/secrun-cli/releases/download/$V/install.sh" | sh -s -- -v "$V"   # Debian/Ubuntu
wget -qO- "https://github.com/ueby/secrun-cli/releases/download/$V/install.sh" | sh -s -- -v "$V"    # Alpine
go install github.com/ueby/secrun-cli/cmd/secrun@latest                                               # Go 1.27+
```

O `install.sh` detecta a arquitetura (amd64, arm64, armv7, armv6, 386, ppc64le,
s390x, riscv64), baixa somente via HTTPS, valida o SHA-256 (e, opcionalmente, um
hash fixado por você com `-s`) e instala em `/usr/local/bin`. Use sempre o
`install.sh` da própria release, com versão fixa. Para imagens Docker
Debian/Alpine, builds multi-stage e a imagem `ghcr.io/ueby/secrun`, veja
**[docs/install.md](docs/install.md)**.

## Uso

```text
secrun run [flags] -- <comando> [argumentos...]
secrun version
secrun help
```

O separador `--` é obrigatório: tudo o que vem depois dele é o comando da aplicação e
não é interpretado pelo `secrun`.

### Flags de `run`

Precedência: **flag > variável de ambiente > padrão**.

| Flag | Variável | Padrão | Descrição |
|---|---|---|---|
| `--secret-id` | `SECRUN_IDS` | — (obrigatório) | Nome ou ARN do segredo. Repetível ou separado por vírgula. |
| `--region` | `SECRUN_REGION` | cadeia do SDK | Região AWS. Sem valor, usa `AWS_REGION`, `AWS_DEFAULT_REGION`, perfil ou IMDS. |
| `--role-arn` | `SECRUN_ROLE_ARN` | — | IAM Role a assumir via STS antes da leitura (cross-account). |
| `--external-id` | `SECRUN_EXTERNAL_ID` | — | `ExternalId` do AssumeRole. Requer `--role-arn`. |
| `--prefix` | `SECRUN_PREFIX` | — | Prefixo aplicado às variáveis injetadas (ex.: `APP_`). |
| `--allow-key` | `SECRUN_ALLOW_KEYS` | — | Libera uma [variável bloqueada](SECURITY.md#variáveis-bloqueadas) (ex.: `NODE_OPTIONS`). Repetível ou separada por vírgula. |
| `--override` | `SECRUN_OVERRIDE` | `true` | Segredos sobrescrevem variáveis já existentes. Use `--override=false` para preservá-las. |
| `--timeout` | `SECRUN_TIMEOUT` | `10s` | Tempo máximo para obter todos os segredos (inclui AssumeRole). |
| `--verbose` | `SECRUN_VERBOSE` | `false` | Logs de diagnóstico no `stderr` (IDs dos segredos e quantidade de variáveis; nunca nomes ou valores). |

Uma variável `SECRUN_*` só é lida quando a flag correspondente não é informada,
então um valor inválido no ambiente não impede o uso da flag.

### Exemplos

```bash
# Um segredo
secrun run --secret-id secrun/checkout-api/prod -- ./server

# Vários segredos: os posteriores sobrescrevem chaves dos anteriores
secrun run --secret-id secrun/shared/prod,secrun/checkout-api/prod -- node dist/server.js

# Configuração apenas por ambiente (ideal para Task Definition / Deployment)
export SECRUN_IDS=secrun/shared/prod,secrun/checkout-api/prod
secrun run -- java -jar app.jar

# Cross-account
secrun run --role-arn arn:aws:iam::888888888888:role/secrun-reader \
           --external-id "$EXTERNAL_ID" \
           --secret-id secrun/checkout-api/prod -- ./server
```

## Formato do segredo

Cada segredo deve ser um **objeto JSON** armazenado como `SecretString`
(convencionalmente com o nome `secrun/<projeto>/<ambiente>`). Valores são normalizados:

| JSON | Variável de ambiente |
|---|---|
| `"texto"` | `texto` |
| `42`, `3.14`, `12345678901234567890` | representação original, **sem perda de precisão** |
| `true` / `false` | `true` / `false` |
| `null` | string vazia |
| objeto / array | JSON compacto (ex.: `{"beta":true}`) |

Exemplo:

```json
{ "DB_HOST": "db.internal", "DB_PORT": 5432, "FEATURE_FLAGS": { "beta": true } }
```

Regras de validação (a execução falha, em vez de ignorar silenciosamente):

- a raiz precisa ser um objeto JSON, sem conteúdo após ele;
- nomes de variável não podem ser vazios nem conter `=` ou caracteres de controle;
- nomes com prefixo `SECRUN_` e [variáveis bloqueadas](SECURITY.md#variáveis-bloqueadas)
  são recusados (as bloqueadas podem ser liberadas com `--allow-key`);
- valores não podem conter NUL;
- segredos binários (`SecretBinary`) não são suportados.

Nomes fora do padrão POSIX estrito (ex.: `spring.datasource.url`) são aceitos.

## Uso em containers

```dockerfile
FROM alpine:3.24 AS secrun
ARG SECRUN_VERSION=v0.1.0
RUN wget -qO- "https://github.com/ueby/secrun-cli/releases/download/${SECRUN_VERSION}/install.sh" \
      | sh -s -- -v "${SECRUN_VERSION}"

FROM node:22-slim
COPY --from=secrun /usr/local/bin/secrun /usr/local/bin/secrun
COPY . /app
WORKDIR /app
ENTRYPOINT ["secrun", "run", "--"]
CMD ["node", "dist/server.js"]
```

> Use sempre a forma *exec* (array JSON) no `ENTRYPOINT`. Na forma *shell*, o PID 1
> seria o `/bin/sh` e o `SIGTERM` não chegaria à aplicação.

Exemplos completos:

- [`examples/docker/`](examples/docker): Debian, Alpine, multi-stage e GHCR
- [`examples/ecs/task-definition.json`](examples/ecs/task-definition.json): ECS/Fargate
- [`examples/eks/irsa.yaml`](examples/eks/irsa.yaml): EKS com IRSA
- [`examples/eks/pod-identity.yaml`](examples/eks/pod-identity.yaml): EKS com Pod Identity
- [`examples/iam/`](examples/iam): políticas IAM mínimas e trust policy cross-account

## Códigos de saída

Quando o `exec` é bem-sucedido, o código de saída passa a ser o da própria aplicação.
Antes disso, o `secrun` usa:

| Código | Significado |
|---|---|
| `0` | `help` / `version` |
| `1` | Falha ao obter ou processar segredos (AWS, JSON, timeout) |
| `2` | Uso incorreto (flags, ausência de `--` ou de segredos) |
| `126` | Comando encontrado, mas não pôde ser executado (sem permissão, diretório etc.) |
| `127` | Comando não encontrado no `PATH` |
| `143` | Interrompido por SIGTERM/SIGINT antes de iniciar a aplicação |

## Desenvolvimento

Contribuições são bem-vindas! Veja o **[CONTRIBUTING.md](CONTRIBUTING.md)** para o
fluxo completo (issue → PR com Conventional Commits → squash merge → release).

Requisitos: Go 1.27+ (o `go.mod` fixa `toolchain go1.27.4`; com `GOTOOLCHAIN=auto` ele
é baixado automaticamente) e, para os testes de instalação, Docker.

```bash
make check         # tidy + fmt + vet + testes (unitários e e2e do binário) com -race
make lint          # golangci-lint
make vuln          # govulncheck
make snapshot      # artefatos de release de todas as arquiteturas em ./dist
make e2e-install   # testa o install.sh em container (E2E_IMAGE=alpine:3.24 E2E_PLATFORM=linux/arm64)
```

### CI/CD

| Workflow | Gatilho | O que faz |
|---|---|---|
| [`ci.yml`](.github/workflows/ci.yml) | push em `main`, PRs, chamado pelo Release | lint (golangci-lint, gofmt, go mod tidy, shellcheck); testes com race em Linux amd64/arm64 e macOS; fuzzing; govulncheck; build de todas as arquiteturas; `install.sh` em Debian 12/13, Ubuntu 24.04 e Alpine × amd64/arm64/armv7; build da imagem para 8 plataformas |
| [`release.yml`](.github/workflows/release.yml) | manual (`workflow_dispatch`, dry-run por padrão), com aprovação no environment `release` | CI completo → semantic-release (versão pelos Conventional Commits, `CHANGELOG.md`, tag) → GitHub Release via GoReleaser (binários, `checksums.txt`, `install.sh`) → imagem `ghcr.io/ueby/secrun` multi-arch; atestação SLSA e SBOM |
| [`pr-title.yml`](.github/workflows/pr-title.yml) | PRs | Valida o título do PR no padrão Conventional Commits (ele vira a mensagem do squash) |
| [`codeql.yml`](.github/workflows/codeql.yml) | push, PRs, semanal | Análise estática (Go e GitHub Actions) |
| [`scorecard.yml`](.github/workflows/scorecard.yml) | push, semanal | OpenSSF Scorecard das práticas de supply chain |
| [`roots-check.yml`](.github/workflows/roots-check.yml) | mensal | Abre uma issue quando há certificados raiz embutidos mais novos (módulo que o Dependabot não atualiza) |

Para publicar uma versão, rode o workflow **Release** (primeiro em dry-run) e
aprove a execução no environment `release`:

```bash
gh workflow run release.yml -R ueby/secrun-cli -f dry_run=true    # mostra a próxima versão
gh workflow run release.yml -R ueby/secrun-cli -f dry_run=false   # publica
```

O semantic-release roda via `npx` apenas dentro do pipeline; o projeto não tem
`package.json`. Detalhes em [CONTRIBUTING.md](CONTRIBUTING.md#releases).

As actions são fixadas por SHA de commit, os workflows usam permissões mínimas e
não persistem credenciais, e o Dependabot mantém Go, actions e imagens
atualizados (veja [Dependências](CONTRIBUTING.md#dependências)).

Estrutura:

```text
cmd/secrun/          ponto de entrada (main) e teste e2e do binário
internal/cli/        parsing de argumentos, fluxo do comando run, códigos de saída
internal/secrets/    busca paralela no Secrets Manager e normalização do JSON
internal/environ/    montagem do ambiente final (prefixo, override, limpeza SECRUN_*)
internal/awsconfig/  configuração do AWS SDK (retry, região, AssumeRole)
internal/version/    metadados de build injetados via -ldflags
internal/fakesm/     Secrets Manager emulado para testes
test/                fakesm (servidor) e teste do install.sh em containers
install.sh           instalador POSIX (Debian, Ubuntu, Alpine, macOS)
docs/                instalação, arquitetura e guia operacional
examples/            Docker, ECS, EKS e IAM
```

Para mais detalhes, veja:

- [docs/install.md](docs/install.md): instalação em Debian/Alpine/Docker
- [docs/architecture.md](docs/architecture.md): decisões de design e fluxo de execução
- [docs/operations.md](docs/operations.md): implantação, IAM e troubleshooting
- [SECURITY.md](SECURITY.md): modelo de ameaças e reporte de vulnerabilidades
- [CONTRIBUTING.md](CONTRIBUTING.md): fluxo de contribuição, Conventional Commits e releases
- [CHANGELOG.md](CHANGELOG.md): gerado automaticamente a cada release
