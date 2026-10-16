// Um servidor de comerciante que recebe os webhooks do Spolly Payments e
// verifica cada um com o SDK (o pacote webhook). É o exemplo de integração, e
// é também o servidor externo com que se prova a entrega de ponta a ponta.
//
//	go run ./examples/receber-webhooks -segredos whsec_...[,whsec_...] [-porta 8099]
//
// Responde 200 a um evento com assinatura válida e 400 a qualquer outro, e
// escreve uma linha por pedido na saída, com quantas vezes aquele evento já
// chegou (as retentativas e o reenvio do painel trazem o mesmo id; um servidor
// a sério deduplica por ele).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spolly-ao/spolly-payments-go/webhook"
)

func main() {
	porta := flag.Int("porta", 8099, "porta onde escutar (0 escolhe uma livre)")
	endereco := flag.String("endereco", "127.0.0.1", "endereço onde escutar")
	segredos := flag.String("segredos", os.Getenv("SPOLLY_WEBHOOK_SECRETS"), "segredos whsec_ aceites, separados por vírgulas")
	tolerancia := flag.Duration("tolerancia", webhook.DefaultTolerance, "diferença máxima entre o t da assinatura e o relógio")
	flag.Parse()

	lista := strings.FieldsFunc(*segredos, func(r rune) bool { return r == ',' || r == ' ' })
	if len(lista) == 0 {
		fmt.Fprintln(os.Stderr, "receber-webhooks: falta -segredos (ou SPOLLY_WEBHOOK_SECRETS), o whsec_ do endpoint")
		os.Exit(2)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(*endereco, strconv.Itoa(*porta)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "receber-webhooks:", err)
		os.Exit(1)
	}
	fmt.Printf("a escutar em %s, a verificar com o SDK spolly-payments-go\n", ln.Addr())
	h := novo(webhook.Verifier{Secrets: lista, Tolerance: *tolerancia}, os.Stdout)
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.Serve(ln))
}

type recetor struct {
	v      webhook.Verifier
	saida  io.Writer
	mu     sync.Mutex
	vistos map[string]int
}

func novo(v webhook.Verifier, saida io.Writer) *recetor {
	return &recetor{v: v, saida: saida, vistos: map[string]int{}}
}

func (r *recetor) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		responder(w, http.StatusMethodNotAllowed, map[string]any{"erro": "só POST"})
		return
	}
	evento, _, err := r.v.ParseRequest(req)
	if err != nil {
		motivo := err.Error()
		var ve *webhook.VerificationError
		if errors.As(err, &ve) {
			motivo = ve.Error()
		}
		r.escrever("RECUSADO pelo SDK: %s", motivo)
		responder(w, http.StatusBadRequest, map[string]any{"erro": "assinatura_invalida", "motivo": motivo})
		return
	}
	r.mu.Lock()
	r.vistos[evento.ID]++
	n := r.vistos[evento.ID]
	r.mu.Unlock()
	r.escrever("RECEBIDO %s %s %s %s, assinatura válida pelo SDK, receção n.º %d%s",
		evento.ID, evento.Type, evento.Mode, evento.MerchantID, n, resumo(evento))
	responder(w, http.StatusOK, map[string]any{"recebido": true, "evento": evento.ID, "assinatura": "válida", "rececao": n})
}

// resumo diz o pagamento de um payment.succeeded ou payment.failed.
func resumo(e webhook.Event) string {
	if e.Type != webhook.EventPaymentSucceeded && e.Type != webhook.EventPaymentFailed {
		return ""
	}
	var p struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Amount struct {
			Amount   json.Number `json:"amount"`
			Currency string      `json:"currency"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(e.Data.Object, &p); err != nil || p.ID == "" {
		return ""
	}
	return fmt.Sprintf(" (%s %s, %s %s)", p.ID, p.Status, p.Amount.Amount, p.Amount.Currency)
}

func (r *recetor) escrever(formato string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.saida, time.Now().Format("15:04:05")+" "+formato+"\n", args...)
}

func responder(w http.ResponseWriter, codigo int, corpo any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(codigo)
	_ = json.NewEncoder(w).Encode(corpo)
}
