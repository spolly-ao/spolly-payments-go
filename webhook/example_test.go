package webhook_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/spolly-ao/spolly-payments-go/webhook"
)

// Um servidor de comerciante que recebe os webhooks: verifica a assinatura
// sobre o corpo bruto, deduplica pelo id, e só responde 2xx quando o evento
// ficou tratado.
func Example() {
	segredo := "whsec_9mLw2TcQe4XbN7aVfKp3Rz" // o do endpoint, do painel
	verificador := webhook.Verifier{Secrets: []string{segredo}}
	tratados := map[string]bool{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evento, _, err := verificador.ParseRequest(r)
		if err != nil {
			http.Error(w, "assinatura inválida", http.StatusBadRequest)
			return
		}
		if tratados[evento.ID] {
			w.WriteHeader(http.StatusOK) // repetição: já está feito
			return
		}
		if evento.Type == webhook.EventPaymentSucceeded {
			var pagamento struct {
				ID                string `json:"id"`
				CheckoutSessionID string `json:"checkout_session_id"`
				Amount            struct {
					Amount   int64  `json:"amount"`
					Currency string `json:"currency"`
				} `json:"amount"`
			}
			if err := json.Unmarshal(evento.Data.Object, &pagamento); err != nil {
				http.Error(w, "objeto ilegível", http.StatusBadRequest)
				return
			}
			fmt.Printf("pago: %s, %d %s\n", pagamento.CheckoutSessionID, pagamento.Amount.Amount, pagamento.Amount.Currency)
		}
		tratados[evento.ID] = true
		w.WriteHeader(http.StatusOK)
	})

	// O que o Spolly Payments envia, para o exemplo correr sozinho.
	corpo := `{"id":"evt_1","type":"payment.succeeded","created_at":"2026-10-16T10:00:00Z","mode":"test","merchant_id":"mer_1","api_version":"2026-10-16","data":{"object":{"id":"pay_1","object":"payment","checkout_session_id":"cs_1","amount":{"amount":1500000,"currency":"AOA"}}}}`
	for range 2 {
		r := httptest.NewRequest(http.MethodPost, "/webhooks/spolly", strings.NewReader(corpo))
		r.Header.Set(webhook.SignatureHeader, webhook.Sign(time.Now(), []byte(corpo), segredo))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		fmt.Println(w.Code)
	}
	// Output:
	// pago: cs_1, 1500000 AOA
	// 200
	// 200
}

func ExampleVerify() {
	corpo := []byte(`{"id":"evt_1","type":"payment.succeeded"}`)
	cabecalho := webhook.Sign(time.Now(), corpo, "whsec_certo")

	fmt.Println(webhook.Verify(corpo, cabecalho, "whsec_certo"))
	err := webhook.Verify(corpo, cabecalho, "whsec_errado")
	fmt.Println(errors.Is(err, webhook.ErrSignatureMismatch))
	// Output:
	// <nil>
	// true
}

// Durante a rotação do segredo no painel, aceitam-se os dois até o servidor
// passar a usar só o novo.
func ExampleVerifier_rotacao() {
	corpo := []byte(`{"id":"evt_1","type":"payment.succeeded"}`)
	cabecalho := webhook.Sign(time.Now(), corpo, "whsec_novo")
	v := webhook.Verifier{Secrets: []string{"whsec_novo", "whsec_antigo"}}
	fmt.Println(v.Verify(corpo, cabecalho))
	// Output: <nil>
}

// Um evento assinado há mais tempo do que a tolerância é recusado, mesmo com a
// assinatura certa: é a defesa contra quem grava um pedido e o repete.
func ExampleVerifier_tolerancia() {
	corpo := []byte(`{"id":"evt_1","type":"payment.succeeded"}`)
	antigo := webhook.Sign(time.Now().Add(-time.Hour), corpo, "whsec_certo")
	err := webhook.Verifier{Secrets: []string{"whsec_certo"}, Tolerance: 10 * time.Minute}.Verify(corpo, antigo)
	fmt.Println(errors.Is(err, webhook.ErrTimestampOutsideTolerance))
	// Output: true
}

func ExampleConstructEvent() {
	corpo := []byte(`{"id":"evt_1","type":"refund.created","created_at":"2026-10-16T10:00:00Z","mode":"test","merchant_id":"mer_1","api_version":"2026-10-16","data":{"object":{"id":"re_1"}}}`)
	e, err := webhook.ConstructEvent(corpo, webhook.Sign(time.Now(), corpo, "whsec_certo"), "whsec_certo")
	fmt.Println(e.ID, e.Type, e.Mode, err)
	// Output: evt_1 refund.created test <nil>
}

// Uma plataforma recebe os eventos das contas ligadas dela nos seus
// webhooks: Account diz de que conta vem cada um, e fica vazio nos eventos
// da própria plataforma.
func ExampleEvent_Account() {
	segredo := "whsec_9mLw2TcQe4XbN7aVfKp3Rz"
	corpo := []byte(`{"id":"evt_2","type":"payout.paid","created_at":"2026-11-30T10:15:00Z","mode":"live","merchant_id":"mer_01JA9BILHETEIRA",` +
		`"application":"app_01JA9BILHETES","account":"mer_01JA9VENDEDOR","api_version":"2026-10-16",` +
		`"data":{"object":{"id":"po_01JA9P","object":"payout","amount":{"amount":2500000,"currency":"AOA"},"status":"paid","created_at":"2026-11-30T10:15:00Z"}}}`)
	evento, err := webhook.ConstructEvent(corpo, webhook.Sign(time.Now(), corpo, segredo), segredo)
	if err != nil {
		fmt.Println(err)
		return
	}
	switch {
	case evento.Account != "" && evento.Type == webhook.EventPayoutPaid:
		fmt.Println("saque pago ao vendedor", evento.Account)
	case evento.Account == "":
		fmt.Println("evento da própria plataforma")
	}
	// Output: saque pago ao vendedor mer_01JA9VENDEDOR
}
