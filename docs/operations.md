# Guia operacional

## Convenção de nomes

```text
secrun/<projeto>/<ambiente>       ex.: secrun/checkout-api/prod
secrun/shared/<ambiente>          valores compartilhados entre projetos
```

Informe os segredos compartilhados **antes** dos específicos, para que os específicos
prevaleçam:

```text
SECRUN_IDS=secrun/shared/prod,secrun/checkout-api/prod
```

## Permissões IAM

Política mínima para a identidade do workload (ECS Task Role, role do IRSA ou do Pod
Identity): [`examples/iam/policy-secretsmanager-read.json`](../examples/iam/policy-secretsmanager-read.json).

- Use o sufixo `-??????` no ARN: o Secrets Manager acrescenta exatamente 6
  caracteres aleatórios ao ARN de cada segredo. Evite `-*`, que também casaria
  `secrun/app/prod-legacy-AbCdEf` e outros segredos com o mesmo prefixo.
- Se o segredo for criptografado com uma **CMK própria**, conceda também `kms:Decrypt`
  (com as condições `kms:ViaService` e `kms:EncryptionContext:SecretARN`). Para a
  chave gerenciada `aws/secretsmanager`, isso não é necessário.
- Restrinja quem pode **escrever** nos segredos (`PutSecretValue`/`UpdateSecret`)
  tanto quanto quem pode fazer deploy. O secrun bloqueia variáveis perigosas
  (ver [SECURITY.md](../SECURITY.md#variáveis-bloqueadas)), mas o conteúdo dos
  segredos continua sendo configuração da aplicação.
- Atenção: no ECS, a permissão vai na **Task Role** (`taskRoleArn`), não na
  *Execution Role*.

### Cross-account

1. Na conta dos segredos (ex.: `888888888888`), crie a role `secrun-reader` com a
   política de leitura e a [trust policy](../examples/iam/trust-policy-cross-account.json)
   que permite a role do workload (opcionalmente exigindo `sts:ExternalId`).
2. Na conta do workload, permita `sts:AssumeRole` nessa role
   ([exemplo](../examples/iam/policy-cross-account-assume-role.json)).
3. Configure `SECRUN_ROLE_ARN` (e `SECRUN_EXTERNAL_ID`, se aplicável).

O `RoleSessionName` usado é `secrun-<hostname>`. No ECS/EKS o hostname corresponde à
task/pod, o que facilita rastrear acessos no CloudTrail.

## ECS

- Defina `SECRUN_IDS` em `environment` da container definition
  ([exemplo](../examples/ecs/task-definition.json)).
- `AWS_REGION` e as credenciais da Task Role já são fornecidas pelo agente do ECS.
- Ajuste `stopTimeout` ao tempo de *graceful shutdown* da aplicação: o `SIGTERM` chega
  diretamente nela.

## EKS

- **IRSA**: anote a ServiceAccount com `eks.amazonaws.com/role-arn`
  ([exemplo](../examples/eks/irsa.yaml)).
- **Pod Identity**: crie a associação com `aws eks create-pod-identity-association`
  ([exemplo](../examples/eks/pod-identity.yaml)).
- Ambos injetam `AWS_REGION` automaticamente. Em outros cenários sem região definida,
  o secrun tenta o IMDS como último recurso; prefira definir `AWS_REGION` ou
  `SECRUN_REGION` explicitamente.
- O secrun funciona com `readOnlyRootFilesystem: true`, pois não escreve em disco.

## Arquiteturas

O binário é estático e publicado para Linux `amd64`, `arm64` (Graviton), `armv7`,
`armv6`, `386`, `ppc64le`, `s390x` e `riscv64`, além de macOS `amd64`/`arm64`.
Veja [install.md](install.md).

## Troubleshooting

Ative os logs de diagnóstico com `SECRUN_VERBOSE=true`. Eles mostram os IDs dos
segredos, a role assumida, o timeout e a quantidade de variáveis injetadas; nomes e
valores não são registrados. Mensagens de erro podem citar nomes de variáveis
válidos, mas nunca valores nem chaves inválidas.

| Mensagem | Causa provável | Ação |
|---|---|---|
| `região AWS não definida` | Nenhuma região configurada, e o metadata da instância EC2 (IMDS), consultado por até 2s, não respondeu (por exemplo, fora da EC2) ou está desativado (`AWS_EC2_METADATA_DISABLED=true`) | Defina `--region`, `SECRUN_REGION` ou `AWS_REGION`. |
| `AccessDeniedException` | Política IAM sem o ARN do segredo, ou permissão na role errada (Execution Role vs. Task Role) | Revise a política; confira o sufixo `-??????`. |
| `ResourceNotFoundException` | Nome do segredo ou região incorretos | Confira `SECRUN_IDS` e a região. |
| `KMS ... AccessDenied` | Segredo com CMK sem `kms:Decrypt` | Adicione a permissão de KMS. |
| `tempo limite de 10s excedido` | Rede/VPC endpoint, throttling ou IMDS inacessível | Verifique rotas/VPC endpoint `secretsmanager`; aumente `SECRUN_TIMEOUT`. |
| `JSON inválido: erro de sintaxe próximo ao byte N` | `SecretString` não é um objeto JSON válido | Corrija o segredo no plano de controle. |
| `segredos binários não são suportados` | Segredo gravado como `SecretBinary` | Regrave como `SecretString` JSON. |
| `nome de variável inválido` | Chave vazia ou com `=` | Renomeie a chave no segredo. |
| `executável não encontrado` (saída 127) | Comando após `--` fora do `PATH` | Use caminho absoluto ou ajuste o `PATH` da imagem. |
| `não é possível executar` (saída 126) | Arquivo sem permissão de execução ou diretório | `chmod +x`, ou corrija o caminho. |
| `variável bloqueada por segurança` | O segredo define `NODE_OPTIONS`, `LD_*`, `PATH`, `HTTPS_PROXY` etc. | Renomeie a chave, use `--prefix` ou libere com `--allow-key NOME`. |
| `usa o prefixo reservado SECRUN_` | O segredo define uma chave `SECRUN_*` | Renomeie a chave. |
| `interrompido por sinal` (saída 143) | SIGTERM/SIGINT antes de a aplicação iniciar (ex.: deploy cancelado) | Normal durante rollbacks; verifique a causa se for recorrente. |
| `separador '--' obrigatório` (saída 2) | `ENTRYPOINT`/`CMD` sem `--` | Use `ENTRYPOINT ["secrun","run","--"]`. |

### Testes locais

O secrun usa a cadeia de credenciais padrão, então funciona com perfis locais:

```bash
AWS_PROFILE=dev secrun run --verbose --secret-id secrun/checkout-api/dev -- env
```

Para apontar para um emulador (ex.: LocalStack):

```bash
AWS_ENDPOINT_URL_SECRETS_MANAGER=http://localhost:4566 \
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_REGION=us-east-1 \
secrun run --secret-id secrun/app/dev -- env
```
