package secrets

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{
			name: "tipos primitivos",
			raw:  `{"S":"texto","I":42,"F":3.14,"T":true,"B":false,"N":null}`,
			want: map[string]string{"S": "texto", "I": "42", "F": "3.14", "T": "true", "B": "false", "N": ""},
		},
		{
			name: "números grandes sem perda de precisão",
			raw:  `{"BIG":12345678901234567890,"EXP":1e21}`,
			want: map[string]string{"BIG": "12345678901234567890", "EXP": "1e21"},
		},
		{
			name: "objetos e arrays viram JSON compacto sem escape de HTML",
			raw:  `{"OBJ":{"a":1,"b":"<x&y>"},"ARR":[1, "dois", null]}`,
			want: map[string]string{"OBJ": `{"a":1,"b":"<x&y>"}`, "ARR": `[1,"dois",null]`},
		},
		{
			name: "objeto vazio",
			raw:  `{}`,
			want: map[string]string{},
		},
		{
			name: "espaços ao redor são aceitos",
			raw:  " \n{\"K\":\"v\"}\n ",
			want: map[string]string{"K": "v"},
		},
		{
			name: "chaves não estritamente POSIX são preservadas",
			raw:  `{"spring.datasource.url":"jdbc","my-key":"x"}`,
			want: map[string]string{"spring.datasource.url": "jdbc", "my-key": "x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tt.raw)
			if err != nil {
				t.Fatalf("Parse() erro inesperado: %v", err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("Parse() = %v, esperado %v", got, tt.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		invalid bool // espera ErrInvalidJSON
	}{
		{name: "vazio", raw: "", invalid: true},
		{name: "sintaxe inválida", raw: `{"K": SUPERSECRETO}`, invalid: true},
		{name: "truncado", raw: `{"K":"SUPERSECRETO"`, invalid: true},
		{name: "raiz array", raw: `["SUPERSECRETO"]`, invalid: true},
		{name: "raiz string", raw: `"SUPERSECRETO"`, invalid: true},
		{name: "raiz número", raw: `987654321`, invalid: true},
		{name: "raiz null", raw: `null`, invalid: true},
		{name: "texto puro", raw: `SUPERSECRETO`, invalid: true},
		{name: "conteúdo extra", raw: `{"K":"v"} {"SUPERSECRETO":1}`, invalid: true},
		{name: "chave vazia", raw: `{"":"SUPERSECRETO"}`},
		{name: "chave com igual", raw: `{"A=B":"SUPERSECRETO"}`},
		{name: "chave com NUL", raw: `{"A\u0000B":"SUPERSECRETO"}`},
		{name: "chave com quebra de linha", raw: `{"A\nB":"SUPERSECRETO"}`},
		{name: "valor colado como chave", raw: `{"DB_PASSWORD=SUPERSECRETO":""}`},
		{name: "token colado como chave", raw: `{"sk-live-SUPERSECRETO=":"v"}`},
		{name: "valor com NUL", raw: `{"K":"SUPER\u0000SECRETO"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(tt.raw)
			if err == nil {
				t.Fatal("Parse() deveria falhar")
			}
			if tt.invalid && !errors.Is(err, ErrInvalidJSON) {
				t.Errorf("erro %v deveria envolver ErrInvalidJSON", err)
			}
			// Garantia de segurança: o conteúdo do segredo nunca aparece no erro.
			for _, leak := range []string{"SUPER", "SECRETO", "987654321"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("mensagem de erro vaza conteúdo do segredo: %q", err)
				}
			}
		})
	}
}

func TestValidateKey(t *testing.T) {
	t.Parallel()

	valid := []string{"A", "DB_PASSWORD", "_x", "spring.profile", "a-b"}
	for _, k := range valid {
		if err := ValidateKey(k); err != nil {
			t.Errorf("ValidateKey(%q) = %v, esperado nil", k, err)
		}
	}
	invalid := []string{"", "A=B", "A\x00B", "A\nB", "A\rB", "A\tB", "A\x7fB"}
	for _, k := range invalid {
		if err := ValidateKey(k); err == nil {
			t.Errorf("ValidateKey(%q) deveria falhar", k)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(`{"K":"v"}`)
	f.Add(`{"N":1.5e10,"O":{"a":[1,2]}}`)
	f.Add(`[1,2,3]`)
	f.Add(`{"K":`)

	f.Fuzz(func(t *testing.T, raw string) {
		got, err := Parse(raw)
		if err != nil {
			return
		}
		for k, v := range got {
			if ValidateKey(k) != nil {
				t.Errorf("chave inválida aceita: %q", k)
			}
			if strings.IndexByte(v, 0) >= 0 {
				t.Errorf("valor com NUL aceito na chave %q", k)
			}
		}
	})
}
