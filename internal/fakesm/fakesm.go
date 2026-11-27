// Package fakesm implementa um servidor HTTP mínimo que emula a operação
// GetSecretValue do AWS Secrets Manager (protocolo JSON 1.1).
//
// Destina-se exclusivamente a testes: o secrun é apontado para ele via
// AWS_ENDPOINT_URL_SECRETS_MANAGER, permitindo validar o binário real (inclusive
// dentro de containers) sem acesso à AWS.
package fakesm

import (
	"encoding/json"
	"net/http"
)

// Handler responde GetSecretValue a partir de secrets (id -> SecretString).
// Ids ausentes retornam ResourceNotFoundException.
func Handler(secrets map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("X-Amz-Target") != "secretsmanager.GetSecretValue" {
			writeError(w, http.StatusBadRequest, "UnknownOperationException", "operação não suportada")
			return
		}

		var in struct{ SecretId string } //nolint:revive // nome definido pela API da AWS
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "SerializationException", "corpo inválido")
			return
		}

		value, ok := secrets[in.SecretId]
		if !ok {
			writeError(w, http.StatusBadRequest, "ResourceNotFoundException",
				"Secrets Manager can't find the specified secret.")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ARN":          "arn:aws:secretsmanager:us-east-1:000000000000:secret:" + in.SecretId + "-AbCdEf",
			"Name":         in.SecretId,
			"SecretString": value,
			"VersionId":    "00000000-0000-0000-0000-000000000000",
		})
	})
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"__type": code, "message": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
