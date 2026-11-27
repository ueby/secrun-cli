package secrets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ErrInvalidJSON indica que o SecretString não é um objeto JSON válido.
//
// As mensagens derivadas deste erro NUNCA incluem trechos do conteúdo do
// segredo: os erros nativos de encoding/json podem citar caracteres ou
// valores do documento (ex.: "invalid character 'x'..." ou
// "cannot unmarshal number 1234..."), por isso são sempre descartados e
// substituídos por descrições genéricas.
var ErrInvalidJSON = errors.New("JSON inválido")

// Parse converte o SecretString de um segredo (um objeto JSON) em um mapa
// de variáveis de ambiente.
//
// Regras de normalização dos valores:
//
//   - string  -> o próprio texto
//   - number  -> a representação textual original (sem perda de precisão,
//     ex.: 12345678901234567890 permanece idêntico)
//   - boolean -> "true" ou "false"
//   - null    -> "" (string vazia)
//   - objeto/array -> JSON compacto, sem escape de HTML
//
// As chaves são validadas com ValidateKey; valores contendo o byte NUL são
// rejeitados, pois não podem ser representados em variáveis de ambiente.
func Parse(raw string) (map[string]string, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()

	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, sanitizeJSONError(err)
	}
	if obj == nil { // documento "null"
		return nil, fmt.Errorf("%w: esperado um objeto JSON no nível raiz", ErrInvalidJSON)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: conteúdo adicional após o objeto JSON", ErrInvalidJSON)
	}

	out := make(map[string]string, len(obj))
	for key, value := range obj {
		if err := ValidateKey(key); err != nil {
			return nil, err
		}
		str, err := stringify(value)
		if err != nil {
			return nil, fmt.Errorf("valor inválido na chave %q: %w", key, err)
		}
		if strings.IndexByte(str, 0) >= 0 {
			return nil, fmt.Errorf("valor inválido na chave %q: contém o caractere NUL", key)
		}
		out[key] = str
	}
	return out, nil
}

// ValidateKey verifica se key pode ser usada como nome de variável de
// ambiente: não pode ser vazia nem conter '=' ou caracteres de controle
// (NUL, quebra de linha etc.).
//
// A validação é intencionalmente permissiva (não exige o padrão POSIX
// estrito [A-Za-z_][A-Za-z0-9_]*) para não quebrar aplicações que usam
// nomes com '.', '-' etc. Apenas o que tornaria o ambiente inválido ou
// ambíguo é rejeitado.
//
// Segurança: a mensagem de erro NUNCA cita a chave, apenas o motivo e o
// tamanho. Uma chave inválida costuma ser conteúdo colado no lugar errado
// (ex.: "DB_PASSWORD=hunter2" vindo de um arquivo .env).
func ValidateKey(key string) error {
	if key == "" {
		return errors.New("nome de variável vazio")
	}
	for i := 0; i < len(key); i++ {
		switch c := key[i]; {
		case c == '=':
			return fmt.Errorf("nome de variável inválido (%d bytes): contém '=' na posição %d", len(key), i)
		case c < 0x20 || c == 0x7f:
			return fmt.Errorf("nome de variável inválido (%d bytes): contém caractere de controle na posição %d", len(key), i)
		}
	}
	return nil
}

func stringify(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case json.Number:
		return v.String(), nil
	default: // map[string]any ou []any
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return "", errors.New("não foi possível serializar o valor aninhado")
		}
		return strings.TrimSuffix(buf.String(), "\n"), nil
	}
}

// sanitizeJSONError traduz erros de encoding/json em mensagens que não
// expõem o conteúdo do segredo. O erro original é descartado de propósito.
func sanitizeJSONError(err error) error {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError

	switch {
	case errors.Is(err, io.EOF):
		return fmt.Errorf("%w: conteúdo vazio", ErrInvalidJSON)
	case errors.Is(err, io.ErrUnexpectedEOF):
		return fmt.Errorf("%w: documento truncado", ErrInvalidJSON)
	case errors.As(err, &syntaxErr):
		return fmt.Errorf("%w: erro de sintaxe próximo ao byte %d", ErrInvalidJSON, syntaxErr.Offset)
	case errors.As(err, &typeErr):
		return fmt.Errorf("%w: esperado um objeto JSON no nível raiz", ErrInvalidJSON)
	default:
		return ErrInvalidJSON
	}
}
