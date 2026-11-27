# Contribuindo

Obrigado pelo interesse em contribuir com o `secrun`! Este guia descreve o fluxo
de desenvolvimento, de revisão e de release do projeto.

> Encontrou uma vulnerabilidade? **Não abra issue pública**: siga o
> [SECURITY.md](SECURITY.md).

## Fluxo de trabalho

```text
issue ──▶ branch ──▶ commits ──▶ pull request ──▶ CI verde ──▶ squash merge ──▶ release manual
```

1. **Issue.** Descreva o problema ou a proposta usando os
   [templates](https://github.com/ueby/secrun-cli/issues/new/choose). Mudanças
   pequenas e óbvias (typos, por exemplo) podem ir direto para PR.
2. **Branch** a partir da `main`, no formato `<tipo>/<issue>-<descricao>`, por
   exemplo `fix/12-armv8l` ou `feat/15-dry-run`.
3. **Commits** pequenos e coesos, de preferência já no padrão
   [Conventional Commits](#conventional-commits).
4. **Pull request** para a `main`, preenchendo o template e referenciando a issue
   (`Closes #12`).
5. **CI verde.** A `main` é protegida: o merge exige todos os checks obrigatórios
   (lint, testes em Linux amd64/arm64 e macOS, govulncheck, build de todas as
   arquiteturas, instalação em Debian/Ubuntu/Alpine, imagem Docker, CodeQL e
   título do PR).
6. **Squash merge.** É o único método permitido. O **título do PR** vira a
   mensagem do commit na `main`, e é ele que define a próxima versão.

## Ambiente de desenvolvimento

Requisitos: Go 1.27+ (o `go.mod` fixa `toolchain go1.27.4`; com `GOTOOLCHAIN=auto`
ele é baixado automaticamente) e Docker para os testes de instalação.

```bash
make check         # tidy + fmt + vet + testes (unitários e e2e) com -race
make lint          # golangci-lint
make vuln          # govulncheck
make fuzz          # fuzzing do parser de segredos
make snapshot      # artefatos de todas as arquiteturas em ./dist
make e2e-install   # install.sh em container (E2E_IMAGE=alpine:3.24 E2E_PLATFORM=linux/arm64)
```

Antes de abrir o PR, rode pelo menos `make check lint`.

### Diretrizes

- **Segurança primeiro:** valores de segredos nunca podem aparecer em logs,
  erros ou testes. Mudanças em `internal/environ/policy.go` (variáveis
  bloqueadas) exigem atualizar o [SECURITY.md](SECURITY.md); um teste garante
  essa sincronia.
- **Testes:** todo comportamento novo ou corrigido precisa de teste. O pacote
  `internal/fakesm` emula o Secrets Manager para testes e2e sem AWS.
- **Dependências:** evite novas. O binário depende só da stdlib, do AWS SDK e de
  `x/crypto/x509roots/fallback`.
- **Documentação:** atualize o README e o `docs/` junto com o código.

## Conventional Commits

Formato do título do PR (validado no CI):

```text
<tipo>(<escopo opcional>)<! opcional>: <descrição>
```

| Tipo | Quando usar | Versão gerada |
|---|---|---|
| `feat` | nova funcionalidade | minor (`0.1.0` → `0.2.0`) |
| `fix` | correção de bug | patch (`0.1.0` → `0.1.1`) |
| `perf` | melhoria de desempenho | patch |
| `feat!`, `fix!` etc. | mudança incompatível (breaking change) | major (`0.x` → `1.0.0`) |
| `docs`, `test`, `refactor`, `build`, `ci`, `chore`, `style`, `revert` | demais mudanças | nenhuma |

Exemplos:

```text
feat(cli): adiciona flag --dry-run
fix(install): detecta armv8l como armv7
feat!: remove a variável SECRUN_REGION
docs: explica o uso com Pod Identity
ci(deps): bump actions/checkout
```

O squash usa **apenas o título**; o corpo do PR não entra na mensagem do commit.
Para sinalizar uma breaking change, use `!` no título e descreva a migração no
corpo do PR.

Os PRs do Dependabot já seguem o padrão: `fix(deps)` para dependências do
binário (gera patch), `ci(deps)` para actions e `build(deps)` para a imagem de
build (não geram versão).

## Releases

Releases são **manuais** e **automatizadas** pelo
[semantic-release](https://semantic-release.gitbook.io/), executado via `npx` só
dentro do pipeline. O repositório não tem `package.json` nem `node_modules`.

### Como publicar

1. Em **Actions → Release → Run workflow**, na `main`, rode primeiro com
   **dry-run** (padrão). O log do passo *semantic-release* mostra a próxima
   versão e as notas, sem publicar nada.
2. Rode de novo com `dry_run` desmarcado.
3. Aprove a execução no environment **`release`**.

Pela CLI:

```bash
gh workflow run release.yml -R ueby/secrun-cli -f dry_run=true
gh workflow run release.yml -R ueby/secrun-cli -f dry_run=false
```

### O que acontece

1. **CI completo:** o mesmo workflow dos PRs, reutilizado.
2. **semantic-release:** analisa os commits desde a última tag `v*`, calcula a
   versão, atualiza o `CHANGELOG.md`, faz o commit `chore(release): X.Y.Z` na
   `main` e cria a tag `vX.Y.Z`. Se nenhum commit gerar versão, o workflow
   termina sem publicar.
3. **GoReleaser:** cria a GitHub Release com as notas do semantic-release, os
   binários de 10 plataformas, o `checksums.txt`, o `install.sh` e a atestação
   de proveniência SLSA.
4. **Imagem:** `ghcr.io/ueby/secrun:X.Y.Z` (e `X.Y`; e `X` a partir da 1.0),
   multi-arch, com SBOM e atestação.

### Segurança do processo

- O commit de release e a tag são enviados pela deploy key `RELEASE_DEPLOY_KEY`,
  secret do environment `release`. Esse environment só aceita execuções a partir
  da `main` e exige aprovação. A deploy key é a **única** exceção às regras de
  proteção da `main` e das tags `v*`; ninguém mais faz push direto.
- As notas da release (vindas das mensagens de commit) são repassadas ao
  GoReleaser por arquivo, codificadas em base64, sem interpolação em shell.
- As versões do semantic-release e dos plugins são fixadas no workflow. Para
  atualizá-las, edite a lista no `release.yml` e valide com um dry-run.
- As releases são imutáveis: depois de publicadas, os artefatos não podem ser
  alterados.

### Manutenção da deploy key

Para rotacionar a chave (por exemplo, a cada 12 meses ou em caso de suspeita de
vazamento):

```bash
ssh-keygen -t ed25519 -N "" -C "secrun-release" -f release_key
gh repo deploy-key add release_key.pub -R ueby/secrun-cli --allow-write --title "release (semantic-release)"
gh secret set RELEASE_DEPLOY_KEY -R ueby/secrun-cli --env release < release_key
rm -f release_key release_key.pub
# depois, remova a chave antiga: gh repo deploy-key list / delete
```
