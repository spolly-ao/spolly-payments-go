// O percurso inteiro de uma integração, com o SDK: confirma a chave, cria uma
// sessão de checkout, espera pelo webhook payment.succeeded dessa sessão
// (verificado com o pacote webhook), e lê a sessão no fim para a dar por paga.
//
//	SPOLLY_SECRET_KEY=sk_test_... SPOLLY_WEBHOOK_SECRETS=whsec_... \
//	  go run ./examples/checkout -api http://localhost:8084 -porta 7400
//
// O endpoint do webhook, no painel, aponta para http://127.0.0.1:<porta>/webhooks/spolly
// e subscreve payment.succeeded. O programa escreve o URL do checkout: abre-se
// e paga-se (em modo teste, "Aprovar"). Sai com 0 quando o webhook chegou com a
// assinatura válida e a sessão está paga, e com 1 em qualquer outro caso.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	spolly "github.com/spolly-ao/spolly-payments-go"
	"github.com/spolly-ao/spolly-payments-go/webhook"
)

func main() {
	api := flag.String("api", envOu("SPOLLY_API_URL", spolly.DefaultBaseURL), "o endereço da API")
	porta := flag.Int("porta", 7400, "a porta onde se recebem os webhooks (0 escolhe uma livre)")
	valor := flag.Int64("valor", 250000, "o valor da sessão, em unidades menores de AOA (250000 é 2.500,00 Kz)")
	espera := flag.Duration("espera", 10*time.Minute, "quanto esperar pelo pagamento")
	flag.Parse()

	cfg := config{
		Chave:    os.Getenv("SPOLLY_SECRET_KEY"),
		Segredos: strings.FieldsFunc(os.Getenv("SPOLLY_WEBHOOK_SECRETS"), func(r rune) bool { return r == ',' || r == ' ' }),
		API:      *api,
		Endereco: net.JoinHostPort("127.0.0.1", strconv.Itoa(*porta)),
		Valor:    *valor,
		Espera:   *espera,
	}
	if err := run(context.Background(), cfg, os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, "checkout:", err)
		os.Exit(1)
	}
}

func envOu(k, omissao string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return omissao
}

type config struct {
	Chave    string
	Segredos []string
	API      string
	Endereco string
	Valor    int64
	Espera   time.Duration
}

// pago é o que o webhook diz de um pagamento.
type pago struct {
	Evento, Pagamento, Sessao string
	Valor                     spolly.Money
}

// run faz o percurso. aberta, se não for nil, recebe a sessão criada e o
// endereço dos webhooks (os testes usam-na para fazer de comprador e de API).
func run(ctx context.Context, cfg config, saida io.Writer, aberta func(sessao *spolly.CheckoutSession, webhooks string)) error {
	if cfg.Chave == "" || len(cfg.Segredos) == 0 {
		return errors.New("faltam SPOLLY_SECRET_KEY (a sk_test_) e SPOLLY_WEBHOOK_SECRETS (o whsec_ do endpoint)")
	}
	cliente := spolly.NewClient(cfg.Chave, spolly.WithBaseURL(cfg.API), spolly.WithUserAgent("exemplo-checkout"))

	// 1. A chave é de quem, e em que modo?
	conta, err := cliente.GetAccount(ctx)
	if err != nil {
		return fmt.Errorf("confirmar a chave: %w", err)
	}
	fmt.Fprintf(saida, "chave de %s (%s), modo %s\n", conta.TradeName, conta.MerchantID, conta.Mode)

	// 2. Quem recebe os webhooks, já à escuta antes de haver sessão.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.Endereco)
	if err != nil {
		return fmt.Errorf("escutar os webhooks: %w", err)
	}
	pagos := make(chan pago, 16)
	srv := &http.Server{Handler: recetor(webhook.Verifier{Secrets: cfg.Segredos}, saida, pagos), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	fmt.Fprintf(saida, "a receber webhooks em http://%s/webhooks/spolly\n", ln.Addr())

	// 3. A sessão. A Idempotency-Key vai sozinha.
	pedido := "sdk-go-" + strconv.FormatInt(time.Now().Unix(), 10)
	sessao, err := cliente.CreateCheckoutSession(ctx, &spolly.CheckoutSessionParams{
		Amount:      cfg.Valor,
		Currency:    spolly.AOA,
		Description: "Ensaio do SDK em Go",
		Metadata:    map[string]string{"order_id": pedido},
	})
	if err != nil {
		return fmt.Errorf("criar a sessão: %w", err)
	}
	fmt.Fprintf(saida, "sessão %s criada, %d %s, %s\n", sessao.ID, sessao.Amount.Amount, sessao.Amount.Currency, sessao.Status)
	fmt.Fprintf(saida, "abre e paga: %s\n", sessao.URL)
	if aberta != nil {
		aberta(sessao, "http://"+ln.Addr().String()+"/webhooks/spolly")
	}

	// 4. O webhook é a fonte da verdade.
	limite := time.NewTimer(cfg.Espera)
	defer limite.Stop()
	for {
		select {
		case <-limite.C:
			return fmt.Errorf("o webhook payment.succeeded da sessão %s não chegou em %s", sessao.ID, cfg.Espera)
		case p := <-pagos:
			if p.Sessao != sessao.ID {
				continue
			}
			fmt.Fprintf(saida, "webhook %s: pagamento %s de %d %s, assinatura válida pelo SDK\n", p.Evento, p.Pagamento, p.Valor.Amount, p.Valor.Currency)
			// 5. E a sessão diz o mesmo.
			lida, err := cliente.GetCheckoutSession(ctx, sessao.ID)
			if err != nil {
				return fmt.Errorf("ler a sessão: %w", err)
			}
			if !lida.Paid() || lida.Payment.ID != p.Pagamento {
				return fmt.Errorf("a sessão %s está %s, e o webhook dizia paga por %s", lida.ID, lida.Status, p.Pagamento)
			}
			fmt.Fprintf(saida, "sessão %s %s, paga por %s (%s)\n", lida.ID, lida.Status, lida.Payment.ID, lida.Payment.Method)
			return nil
		}
	}
}

// recetor verifica cada webhook com o SDK e manda os payment.succeeded para o
// canal. Responde 400 a uma assinatura que não bate, e 200 ao resto.
func recetor(v webhook.Verifier, saida io.Writer, pagos chan<- pago) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evento, _, err := v.ParseRequest(r)
		if err != nil {
			fmt.Fprintf(saida, "webhook recusado pelo SDK: %v\n", err)
			http.Error(w, "assinatura inválida", http.StatusBadRequest)
			return
		}
		if evento.Type == webhook.EventPaymentSucceeded {
			var p struct {
				ID                string       `json:"id"`
				CheckoutSessionID string       `json:"checkout_session_id"`
				Amount            spolly.Money `json:"amount"`
			}
			if err := json.Unmarshal(evento.Data.Object, &p); err != nil {
				http.Error(w, "objeto ilegível", http.StatusBadRequest)
				return
			}
			select {
			case pagos <- pago{Evento: evento.ID, Pagamento: p.ID, Sessao: p.CheckoutSessionID, Valor: p.Amount}:
			default: // ninguém à espera: o percurso já acabou
			}
		}
		w.WriteHeader(http.StatusOK)
	})
}
