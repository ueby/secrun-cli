// Command secrun injeta segredos do AWS Secrets Manager como variáveis de
// ambiente e, em seguida, substitui a si mesmo pelo processo da aplicação.
//
// Foi projetado para ser o ENTRYPOINT de containers em Amazon ECS e Amazon
// EKS, usando a identidade IAM nativa do workload (ECS Task Role, EKS IRSA ou
// EKS Pod Identity), sem credenciais estáticas e sem gravar nada em disco.
//
// Uso:
//
//	secrun run [flags] -- <comando> [argumentos...]
//	secrun version
//	secrun help
//
// Consulte o README.md para a documentação completa.
package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	// Certificados raiz da Mozilla embutidos, usados SOMENTE quando o sistema
	// não possui um bundle de CAs (scratch, busybox, debian-slim sem
	// ca-certificates). Garante HTTPS com a AWS em qualquer imagem base.
	_ "golang.org/x/crypto/x509roots/fallback"

	"github.com/ueby/secrun-cli/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, release := notifyContext(os.Interrupt, syscall.SIGTERM)
	defer release()

	app := cli.New()
	app.ReleaseSignals = release
	return app.Run(ctx, os.Args[1:])
}

// notifyContext retorna um contexto cancelado ao receber um dos sinais, para
// abortar de forma limpa a busca de segredos caso o orquestrador envie
// SIGTERM/SIGINT antes de a aplicação ser iniciada.
//
// Diferente de signal.NotifyContext, a função de liberação NÃO cancela o
// contexto: ela devolve os sinais ao comportamento padrão e garante que um
// sinal recebido até aquele momento já esteja refletido em ctx.Err(). Isso
// permite ao chamador decidir, sem condição de corrida, se ainda pode fazer
// o exec da aplicação.
func notifyContext(sig ...os.Signal) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	quit := make(chan struct{})
	var wg sync.WaitGroup

	signal.Notify(ch, sig...)
	wg.Go(func() {
		select {
		case <-ch:
			cancel()
		case <-quit:
		}
	})

	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			signal.Stop(ch)
			close(quit)
			wg.Wait()
			select { // sinal entregue entre o Stop e o fim da goroutine
			case <-ch:
				cancel()
			default:
			}
		})
	}
}
