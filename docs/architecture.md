# Arquitetura

## Contexto

O `secrun` é o **plano de dados** de uma arquitetura desacoplada de gerenciamento de
segredos em ambientes AWS multi-account (ECS e EKS).

| Plano | Responsabilidade |
|---|---|
| **Controle** (UI web / API / GitOps) | RBAC de pessoas, fluxo de aprovação (*change requests*), auditoria e gravação do JSON final em `secrun/<projeto>/<ambiente>` no Secrets Manager de cada conta. |
| **Dados** (`secrun`) | Roda dentro do container, lê os segredos **diretamente** do Secrets Manager com a identidade IAM do workload e inicia a aplicação. |

Não existe backend próprio no caminho crítico (*hot path*) de inicialização dos
containers: a disponibilidade depende apenas do Secrets Manager e do STS/credenciais
do orquestrador.

## Fluxo de execução

```text
secrun run [flags] -- cmd args...
  │
  ├─ 1. parseRunArgs     flags > SECRUN_* > padrões; valida '--', ids, timeout, prefixo
  ├─ 2. context.WithTimeout(--timeout)
  ├─ 3. awsconfig.Load   cadeia de credenciais padrão + retry + região (+ AssumeRole)
  ├─ 4. Fetcher.FetchAll GetSecretValue em paralelo; mescla na ordem dos ids
  │                      (falha rápida: cancela as demais na primeira falha)
  ├─ 5. environ.Build    remove SECRUN_*, valida nomes (lista de bloqueio,
  │                      --allow-key), aplica prefixo/override, ordena
  ├─ 6. exec.LookPath    resolve o binário com o PATH do container
  ├─ 7. libera sinais    SIGTERM recebido até aqui → saída 143, sem exec
  └─ 8. syscall.Exec     substitui o processo (mesmo PID, novo ambiente)
```

## Decisões de design

### `syscall.Exec` em vez de processo filho

Com `exec`, o processo da aplicação herda o PID do `secrun` (PID 1 no container).
Isso dispensa repasse de sinais, coleta de zumbis e propagação do código de saída:
o orquestrador conversa diretamente com a aplicação. Consequência: nada do `secrun`
roda depois que a aplicação inicia (sem refresh/rotação em tempo de execução).

### Busca paralela com mesclagem ordenada

Os segredos são lidos em paralelo (menor latência de inicialização), mas o resultado
é mesclado na ordem informada, então o comportamento "posterior sobrescreve anterior"
é determinístico (ex.: `secrun/shared/prod,secrun/checkout-api/prod`).

A API `BatchGetSecretValue` não foi usada porque exige uma permissão IAM adicional
(`secretsmanager:BatchGetSecretValue`) e não traria ganho relevante para poucos
segredos.

### Erros sem conteúdo de segredo

Os erros de `encoding/json` podem citar trechos do documento (ex.:
`invalid character 'x'` ou `cannot unmarshal number 1234`). Por isso `secrets.Parse`
**descarta** o erro original e devolve apenas descrições genéricas com o offset em
bytes. Essa garantia é coberta por testes e por fuzzing.

### Números sem perda de precisão

O decoder usa `UseNumber()`, preservando a representação textual original. Sem isso,
`12345678901234567890` seria convertido para `float64` e viraria
`1.2345678901234567e+19`.

### Falhar em vez de ignorar

Chaves inválidas (vazias, com `=` ou NUL) e valores com NUL interrompem a execução.
Ignorá-las silenciosamente faria a aplicação subir sem uma configuração esperada,
um problema mais difícil de diagnosticar do que uma falha explícita na inicialização.

### Dependências mínimas

A CLI usa somente a biblioteca padrão (`flag`, `log/slog`), o AWS SDK for Go v2 e
`golang.org/x/crypto/x509roots/fallback`. Este último embute os certificados raiz
da Mozilla, usados apenas quando a imagem não tem `ca-certificates`.
O binário estático tem cerca de 10 MB e não depende de libc (`CGO_ENABLED=0`),
podendo ser copiado para qualquer imagem Linux, inclusive `scratch` e `distroless`.

### Testabilidade

`cli.App` recebe como dependências `Getenv`, `Environ`, `LookPath`, `Exec` e
`NewFetcher`. Os testes exercitam o fluxo completo sem acessar a AWS e sem substituir
o processo de teste. `secrets.Client` é uma interface com o único método usado
(`GetSecretValue`), satisfeita por `*secretsmanager.Client`.

## Configuração de resiliência

| Parâmetro | Valor | Onde |
|---|---|---|
| Tentativas por chamada | 5 | `awsconfig.DefaultMaxAttempts` |
| Backoff máximo | 2s (exponencial com jitter) | `awsconfig.DefaultMaxBackoff` |
| Timeout por tentativa | `--timeout`/3, entre 1s e 5s | `cli.attemptTimeout` |
| Consulta da região ao IMDS | até 2s, no máximo 2 tentativas (só sem região configurada) | `awsconfig.IMDSRegionTimeout` |
| Timeout total | 10s (configurável) | `--timeout` / `SECRUN_TIMEOUT` |

O timeout total cobre todo o processo: resolução de região, AssumeRole e todas as
leituras. O timeout por tentativa garante que uma conexão travada (VPC endpoint
ou ENI sem resposta) seja abortada a tempo de o retry agir. Isso é coberto por
um teste e2e.

### Sinais

Antes do exec, SIGINT/SIGTERM cancelam a busca e o `secrun` sai com 143 sem
iniciar a aplicação. Imediatamente antes do exec, os sinais voltam ao
comportamento padrão; um sinal recebido até ali impede o exec. Assim, um
deploy cancelado durante a inicialização nunca deixa a aplicação subir.

### Variáveis bloqueadas

Um segredo é configuração, mas quem consegue escrever nele não deveria
conseguir executar código. Por isso `environ` recusa, por padrão, variáveis que
mudam o carregador ou os interpretadores (`LD_*`, `NODE_OPTIONS`...), a
identidade AWS (`AWS_ACCESS_KEY_ID`, `AWS_CONTAINER_*`...) ou o tráfego
(`HTTPS_PROXY`, `SSL_CERT_FILE`...). A lista está em
`internal/environ/policy.go`, e um teste garante que ela está documentada no
[SECURITY.md](../SECURITY.md#variáveis-bloqueadas).

## Fora do escopo (por design)

- Rotação ou refresh de segredos após a inicialização (consequência do `exec`).
- Escrita de segredos em arquivos.
- Segredos binários (`SecretBinary`).
- Outros provedores além do AWS Secrets Manager.
