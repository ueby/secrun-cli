<!--
O título do PR vira a mensagem do commit na main (squash) e define a próxima
versão. Use Conventional Commits, por exemplo:
  feat(cli): adiciona flag --foo      → minor
  fix(install): corrige armv8l        → patch
  feat!: remove a flag --bar          → major (descreva a migração abaixo)
  docs|test|refactor|ci|chore: ...    → sem nova versão
Veja o CONTRIBUTING.md.
-->

## O que muda

<!-- Resumo objetivo da mudança e da motivação. -->

Closes #

## Como foi testado

<!-- Testes adicionados/ajustados e comandos executados (make check, make lint, make e2e-install...). -->

## Checklist

- [ ] Título no padrão Conventional Commits
- [ ] Testes cobrindo o comportamento novo ou corrigido
- [ ] `make check` e `make lint` passando localmente
- [ ] Documentação atualizada (README, `docs/`, SECURITY.md se mexer em variáveis bloqueadas)
- [ ] Nenhum valor de segredo em logs, erros ou testes
- [ ] Breaking change sinalizada com `!` no título, com o caminho de migração descrito acima
