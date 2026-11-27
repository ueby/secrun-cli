package environ

import (
	"slices"
	"strings"
)

// Variáveis que um segredo NÃO pode definir por padrão.
//
// Motivo: quem consegue escrever no segredo conseguiria, através delas,
// executar código na aplicação (carregador dinâmico, opções de interpretador,
// funções exportadas do bash), trocar a identidade AWS da aplicação ou
// redirecionar/interceptar o tráfego dela. Com o bloqueio, a permissão de
// escrita no Secrets Manager não equivale à permissão de deploy.
//
// A comparação ignora maiúsculas/minúsculas. Exceções intencionais são
// liberadas com --allow-key / SECRUN_ALLOW_KEYS. Variáveis que já existem no
// ambiente do container (Task Definition, Deployment) não são afetadas.
var (
	deniedExact = []string{
		// Shell e busca de executáveis
		"PATH", "IFS", "ENV", "BASH_ENV", "SHELLOPTS", "BASHOPTS", "PS4",
		// Carregadores de bibliotecas e glibc
		"GCONV_PATH", "LOCPATH", "NLSPATH", "HOSTALIASES", "RESOLV_HOST_CONF",
		// Opções de interpretadores e runtimes
		"NODE_OPTIONS", "NODE_PATH", "NODE_EXTRA_CA_CERTS",
		"JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS", "JDK_JAVA_OPTIONS",
		"PYTHONPATH", "PYTHONHOME", "PYTHONSTARTUP", "PYTHONUSERBASE",
		"PERL5OPT", "PERL5LIB", "PERLLIB", "RUBYOPT", "RUBYLIB",
		"DOTNET_STARTUP_HOOKS", "CORECLR_PROFILER_PATH", "GODEBUG",
		// Confiança TLS e proxies (interceptação/redirecionamento de tráfego)
		"SSL_CERT_FILE", "SSL_CERT_DIR", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		// Identidade e endpoints AWS da aplicação
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"AWS_PROFILE", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE",
		"AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_CA_BUNDLE",
		"AWS_EC2_METADATA_DISABLED", "AWS_EC2_METADATA_SERVICE_ENDPOINT",
	}
	deniedPrefixes = []string{
		"LD_", "DYLD_", "BASH_FUNC_", "AWS_CONTAINER_", "AWS_ENDPOINT_URL",
	}
)

// IsDenied informa se name está na lista de variáveis bloqueadas por padrão.
func IsDenied(name string) bool {
	upper := strings.ToUpper(name)
	if slices.Contains(deniedExact, upper) {
		return true
	}
	for _, p := range deniedPrefixes {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
}

// DeniedNames retorna a lista bloqueada (nomes exatos e prefixos com "*"),
// para documentação e mensagens de ajuda.
func DeniedNames() []string {
	out := slices.Clone(deniedExact)
	for _, p := range deniedPrefixes {
		out = append(out, p+"*")
	}
	return out
}
