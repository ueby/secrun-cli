package cli

const usage = `secrun — injeta segredos do AWS Secrets Manager no ambiente de uma aplicação.

Uso:
  secrun <comando> [argumentos]

Comandos:
  run      Busca os segredos e executa a aplicação (substitui o processo atual)
  version  Exibe a versão do secrun
  help     Exibe esta ajuda

Use 'secrun run --help' para ver as opções do comando run.
`

const runUsage = `Uso:
  secrun run [flags] -- <comando> [argumentos...]

Busca um ou mais segredos (objetos JSON) no AWS Secrets Manager, injeta cada
chave como variável de ambiente e substitui o processo atual pelo <comando>
(syscall.exec), preservando o PID 1 e a entrega de sinais no container.

Flags (precedência: flag > variável de ambiente > padrão):
  --secret-id <id>     Nome ou ARN do segredo. Pode ser repetida ou separada
                       por vírgula; segredos posteriores sobrescrevem chaves
                       dos anteriores.                        [env: SECRUN_IDS]
  --region <região>    Região AWS. Padrão: resolvida pelo SDK (AWS_REGION,
                       AWS_DEFAULT_REGION, perfil ou IMDS).  [env: SECRUN_REGION]
  --role-arn <arn>     IAM Role a assumir via STS antes da leitura
                       (cross-account).                      [env: SECRUN_ROLE_ARN]
  --external-id <id>   ExternalId do AssumeRole; requer --role-arn.
                                                             [env: SECRUN_EXTERNAL_ID]
  --prefix <prefixo>   Prefixo adicionado às variáveis injetadas (ex.: APP_).
                                                             [env: SECRUN_PREFIX]
  --allow-key <nome>   Libera uma variável bloqueada por segurança (ex.:
                       NODE_OPTIONS). Repetível ou separada por vírgula.
                                                             [env: SECRUN_ALLOW_KEYS]
  --override           Segredos sobrescrevem variáveis já existentes
                       (padrão: true; use --override=false).  [env: SECRUN_OVERRIDE]
  --timeout <duração>  Tempo máximo para obter os segredos (padrão: 10s).
                                                             [env: SECRUN_TIMEOUT]
  --verbose            Logs de diagnóstico no stderr (nunca exibe valores).
                                                             [env: SECRUN_VERBOSE]

Todas as variáveis SECRUN_* são removidas antes de iniciar a aplicação.

Por segurança, segredos não podem definir variáveis que alteram a execução,
a identidade AWS ou o tráfego da aplicação (LD_*, PATH, NODE_OPTIONS,
JAVA_TOOL_OPTIONS, PYTHONPATH, HTTPS_PROXY, AWS_ACCESS_KEY_ID, ...), a menos
que liberadas com --allow-key. Prefira --prefix quando possível.

Códigos de saída: 1 falha nos segredos, 2 uso incorreto, 126 comando não
executável, 127 comando não encontrado, 143 interrompido por sinal. Após o
exec, vale o código da própria aplicação.

Exemplos:
  secrun run --secret-id secrun/shared/prod --secret-id secrun/checkout-api/prod -- node dist/server.js
  SECRUN_IDS=secrun/shared/prod,secrun/checkout-api/prod secrun run -- ./app
`
