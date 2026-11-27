// Command fakesm sobe um Secrets Manager falso para testes de integração.
//
//	go run ./test/fakesm -addr 127.0.0.1:18765 -secrets test/fakesm/secrets.json
//
// O arquivo de segredos é um objeto JSON id -> conteúdo do segredo (objeto),
// que é serializado como SecretString.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ueby/secrun-cli/internal/fakesm"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18765", "endereço de escuta")
	file := flag.String("secrets", "test/fakesm/secrets.json", "arquivo JSON com os segredos")
	flag.Parse()

	raw, err := os.ReadFile(*file)
	if err != nil {
		log.Fatalf("fakesm: %v", err)
	}
	var docs map[string]json.RawMessage
	if err := json.Unmarshal(raw, &docs); err != nil {
		log.Fatalf("fakesm: %s: %v", *file, err)
	}
	secrets := make(map[string]string, len(docs))
	for id, doc := range docs {
		secrets[id] = string(doc)
	}

	srv := &http.Server{Addr: *addr, Handler: fakesm.Handler(secrets), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("fakesm: escutando em http://%s (%d segredos)", *addr, len(secrets))
	log.Fatal(srv.ListenAndServe())
}
