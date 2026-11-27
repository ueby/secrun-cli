# Política de segurança

## Reportando uma vulnerabilidade

**Não abra issues públicas para vulnerabilidades.**

Use o [reporte privado de vulnerabilidades do GitHub](https://github.com/ueby/secrun-cli/security/advisories/new).
Inclua a descrição, os passos para reproduzir, o impacto e a versão afetada
(`secrun version`).

| Etapa | Prazo |
|---|---|
| Confirmação de recebimento | até 3 dias úteis |
| Avaliação inicial e severidade | até 10 dias úteis |
| Correção de vulnerabilidades críticas/altas | o mais rápido possível, com advisory e CVE quando aplicável |

## Versões suportadas

Correções de segurança são publicadas apenas para a **última versão minor**.
Mantenha o `secrun` atualizado (o Dependabot pode fazer isso por você ao fixar a
versão no Dockerfile).

## Modelo de ameaças

O `secrun` roda como PID 1 do container, com a identidade IAM do workload, e
entrega segredos à aplicação via variáveis de ambiente.

### Garantias

| Garantia | Implementação |
|---|---|
| Sem credenciais estáticas | Usa apenas a cadeia de credenciais padrão do AWS SDK (Task Role, IRSA, Pod Identity). |
| Mínimo privilégio | Requer só `secretsmanager:GetSecretValue` nos ARNs necessários (e `kms:Decrypt` para CMKs). Veja [examples/iam](examples/iam). |
| Sem persistência | Nada é gravado em disco; funciona com `readOnlyRootFilesystem`. |
| Valores nunca em logs | Erros de JSON são sanitizados (sem trechos do conteúdo); chaves inválidas não são citadas (podem ser um valor colado por engano); o modo `--verbose` registra só IDs de segredos e contagens. Nomes de variáveis **válidas** podem aparecer em mensagens de erro. |
| Configuração interna isolada | Variáveis `SECRUN_*` são removidas antes do exec, e segredos não podem defini-las. |
| Escrita no segredo ≠ execução de código | Segredos não podem definir, por padrão, variáveis que alteram a execução, a identidade ou o tráfego da aplicação (ver abaixo). |
| Binário resolvido com o PATH do container | O comando após `--` é localizado com o `PATH` original, nunca com um valor vindo de segredo. |
| Supply chain | Releases com `checksums.txt` e atestação de proveniência SLSA (Sigstore); imagem com SBOM e proveniência; actions fixadas por SHA; imagens base fixadas por digest. |

### Variáveis bloqueadas

Quem consegue escrever num segredo poderia, sem esse controle, executar código
na aplicação: `NODE_OPTIONS=--require ...`, `LD_PRELOAD`, `BASH_FUNC_*`,
`JAVA_TOOL_OPTIONS` etc. Também poderia trocar a identidade AWS da aplicação ou
redirecionar seu tráfego. Por isso, por padrão, um segredo **não pode** definir:

- **Carregador/shell:** `LD_*`, `DYLD_*`, `BASH_FUNC_*`, `BASH_ENV`, `ENV`, `PATH`, `IFS`, `SHELLOPTS`, `BASHOPTS`, `PS4`, `GCONV_PATH`, `LOCPATH`, `NLSPATH`, `HOSTALIASES`, `RESOLV_HOST_CONF`
- **Runtimes:** `NODE_OPTIONS`, `NODE_PATH`, `JAVA_TOOL_OPTIONS`, `_JAVA_OPTIONS`, `JDK_JAVA_OPTIONS`, `PYTHONPATH`, `PYTHONHOME`, `PYTHONSTARTUP`, `PYTHONUSERBASE`, `PERL5OPT`, `PERL5LIB`, `PERLLIB`, `RUBYOPT`, `RUBYLIB`, `DOTNET_STARTUP_HOOKS`, `CORECLR_PROFILER_PATH`, `GODEBUG`
- **TLS e proxies:** `SSL_CERT_FILE`, `SSL_CERT_DIR`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`, `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`
- **AWS:** `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_PROFILE`, `AWS_CONFIG_FILE`, `AWS_SHARED_CREDENTIALS_FILE`, `AWS_ROLE_ARN`, `AWS_WEB_IDENTITY_TOKEN_FILE`, `AWS_CA_BUNDLE`, `AWS_EC2_METADATA_DISABLED`, `AWS_EC2_METADATA_SERVICE_ENDPOINT`, `AWS_CONTAINER_*`, `AWS_ENDPOINT_URL*`

A comparação ignora maiúsculas/minúsculas e considera o nome final (com
`--prefix`). Se precisar de uma delas, libere explicitamente com
`--allow-key NOME` (ou `SECRUN_ALLOW_KEYS`), de preferência definida na Task
Definition/Deployment, que tem controle de mudança próprio. Usar `--prefix`
neutraliza o problema por completo.

Variáveis definidas diretamente no container (Task Definition, Deployment) não
são afetadas.

### Limitações conhecidas

- **Variáveis de ambiente são visíveis** para o próprio processo e seus filhos, e
  via `/proc/<pid>/environ` para o mesmo usuário (ou root). Isso é inerente à
  injeção via ambiente. Execute como usuário não-root e restrinja
  `docker exec`/`kubectl exec` em produção.
- Ferramentas de diagnóstico da aplicação (dumps, endpoints de debug, `printenv`
  em logs) podem expor o ambiente. Revise-as.
- Não há rotação em tempo de execução: alterar um segredo requer reiniciar a
  task/pod.
- Mensagens de erro do AWS SDK repetem campos da resposta da API. Com o
  endpoint oficial da AWS isso não inclui valores de segredos, mas a garantia
  depende de respostas bem-formadas.
- O `ExternalId` do AssumeRole não é um segredo e pode ficar em texto claro na
  Task Definition.

## Verificando o que você instala

```bash
# Proveniência dos binários (Sigstore + GitHub Attestations)
gh attestation verify secrun_linux_amd64.tar.gz -R ueby/secrun-cli

# Proveniência da imagem
gh attestation verify oci://ghcr.io/ueby/secrun:0.1.0 -R ueby/secrun-cli

# Hash fixado no build (falha se a release for alterada)
sh install.sh -v v0.1.0 -s <sha256 do secrun_linux_amd64.tar.gz>
```
