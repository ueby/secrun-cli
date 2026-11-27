# Changelog

## Escopo inicial (incluído na v0.1.0)

As entradas acima desta seção são geradas automaticamente pelo semantic-release a
partir de [Conventional Commits](https://www.conventionalcommits.org/pt-br/), a cada
release (veja o [CONTRIBUTING.md](CONTRIBUTING.md#releases)). O projeto adota
[Versionamento Semântico](https://semver.org/lang/pt-BR/).

O escopo inicial da primeira versão:

- Comando `secrun run [flags] -- <comando>`, que injeta segredos do AWS Secrets
  Manager no ambiente e substitui o processo via `exec`, preservando o PID 1.
- Múltiplos segredos via `--secret-id` (repetido ou separado por vírgula) ou
  `SECRUN_IDS`, com busca em paralelo e mesclagem ordenada (o último prevalece).
- AssumeRole cross-account opcional (`--role-arn`, `--external-id`), com
  `RoleSessionName` rastreável (`secrun-<hostname>`).
- Flags `--region`, `--prefix`, `--override`, `--timeout`, `--verbose` e
  `--allow-key`, todas com variável `SECRUN_*` equivalente (flag > env > padrão).
- Retry exponencial com timeout por tentativa e prazo total rígido.
- Normalização de valores JSON sem perda de precisão numérica.
- Segurança:
  - valores de segredos nunca aparecem em logs ou erros;
  - chaves inválidas (vazias, com `=` ou caracteres de controle) são rejeitadas
    sem serem citadas;
  - variáveis que alteram execução, identidade AWS ou tráfego (`LD_*`,
    `NODE_OPTIONS`, `PATH`, `HTTPS_PROXY`, `AWS_ACCESS_KEY_ID`...) são bloqueadas
    por padrão, com exceções explícitas via `--allow-key`;
  - variáveis `SECRUN_*` são removidas e não podem ser definidas por segredos;
  - certificados CA embutidos como fallback para imagens sem `ca-certificates`.
- Tratamento de sinais: SIGTERM/SIGINT antes do exec encerra com 143 sem
  iniciar a aplicação.
- Códigos de saída padronizados (1, 2, 126, 127, 143).
- `install.sh` POSIX (Debian, Ubuntu, Alpine, macOS): somente HTTPS, validação
  SHA-256, hash fixável (`-s`), validação de versão e instalação atômica.
- Binários para Linux (amd64, arm64, armv7, armv6, 386, ppc64le, s390x, riscv64)
  e macOS (amd64, arm64), e imagem `ghcr.io/ueby/secrun` multi-arch.
- Atestações de proveniência SLSA para binários e imagem; SBOM da imagem.
- CI com lint, testes em Linux amd64/arm64 e macOS, fuzzing, govulncheck,
  CodeQL, OpenSSF Scorecard e teste do instalador em Debian/Ubuntu/Alpine.
