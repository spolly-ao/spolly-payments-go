# spolly-payments-go

O SDK em Go do [Spolly Payments](https://github.com/spolly-ao/spolly-payments):
criar e ler sessões de checkout, estornar, confirmar a conta da chave, e
verificar a assinatura dos webhooks. Só a biblioteca padrão, Go 1.26 ou mais
recente.

```bash
go get github.com/spolly-ao/spolly-payments-go@v0.3.0
```

O repositório é público: o `go` vai buscá-lo pelo proxy público, sem
credenciais nem variáveis especiais.

## Os nomes, que não mudam

| Pacote | Nome | O que é |
|---|---|---|
| `spolly` | `NewClient(chave, ...Option)` | O cliente. Opções: `WithBaseURL`, `WithTimeout`, `WithMaxRetries`, `WithHTTPClient`, `WithUserAgent` |
| `spolly` | `(*Client).CreateCheckoutSession(ctx, *CheckoutSessionParams, ...RequestOption)` | `POST /v1/checkout/sessions` |
| `spolly` | `(*Client).GetCheckoutSession(ctx, id)` | `GET /v1/checkout/sessions/{id}` |
| `spolly` | `(*Client).CreateRefund(ctx, *RefundParams, ...RequestOption)` | `POST /v1/refunds` |
| `spolly` | `(*Client).GetRefund(ctx, id)` | `GET /v1/refunds/{id}` |
| `spolly` | `(*Client).GetAccount(ctx)` | `GET /v1/conta` |
| `spolly` | `(*Client).GetSubscription(ctx, id)` | `GET /v1/subscriptions/{id}` |
| `spolly` | `(*Client).CancelSubscription(ctx, id, *CancelSubscriptionParams, ...RequestOption)` | `POST /v1/subscriptions/{id}/cancel` (no fim do ciclo, por omissão) |
| `spolly` | `(*Client).CreateSubscriptionPortalLink(ctx, id, ...RequestOption)` | `POST /v1/subscriptions/{id}/portal`: o link de uso único da página do cliente |
| `spolly` | `WithIdempotencyKey(k)`, `NewIdempotencyKey()` | A `Idempotency-Key` de um `POST` |
| `spolly` | `*Error` (`StatusCode`, `Code`, `Message`, `Fields`, `IdempotencyKey`), `ErrorCode(err)`, `Code*` | As recusas da API |
| `spolly` | `ErrInvalidResponse`, `ErrNoKey` | Uma resposta que não se lê; chamar sem chave |
| `spolly` | `Money`, `AOA`, `USD`, `EUR`, `Method*`, `Mode*`, `Session*`, `Refund*`, `Subscription*`, `Kind*`, `Interval*` | Os tipos e os valores do contrato |
| `webhook` | `Verifier`, `Verify`, `ConstructEvent`, `(Verifier).ParseRequest`, `Sign` | A assinatura dos webhooks |
| `webhook` | `*VerificationError`, `Err*`, `Event`, `Event*` | As recusas, o envelope e os tipos de evento |

## Início rápido

```go
import (
	spolly "github.com/spolly-ao/spolly-payments-go"
	"github.com/spolly-ao/spolly-payments-go/webhook"
)

cliente := spolly.NewClient(os.Getenv("SPOLLY_SECRET_KEY")) // sk_test_... ou sk_live_...

// 1. Criar a sessão e mandar o comprador para o checkout alojado.
sessao, err := cliente.CreateCheckoutSession(ctx, &spolly.CheckoutSessionParams{
	Amount:      1500000, // 15.000,00 Kz, em unidades menores
	Currency:    spolly.AOA,
	Description: "Vestido de capulana",
	SuccessURL:  "https://kiala.ao/obrigado?pedido=1042",
	Metadata:    map[string]string{"order_id": "1042"},
})
if err != nil {
	return err
}
http.Redirect(w, r, sessao.URL, http.StatusSeeOther)

// 2. O webhook é a fonte da verdade: só ele dá a encomenda por paga.
v := webhook.Verifier{Secrets: []string{os.Getenv("SPOLLY_WEBHOOK_SECRET")}}
http.HandleFunc("/webhooks/spolly", func(w http.ResponseWriter, r *http.Request) {
	evento, _, err := v.ParseRequest(r) // lê o corpo bruto e verifica Spolly-Signature
	if err != nil {
		http.Error(w, "assinatura inválida", http.StatusBadRequest)
		return
	}
	if evento.Type == webhook.EventPaymentSucceeded {
		var p struct {
			ID                string `json:"id"`
			CheckoutSessionID string `json:"checkout_session_id"`
		}
		_ = json.Unmarshal(evento.Data.Object, &p)
		marcarPaga(p.CheckoutSessionID) // idempotente: deduplicar por evento.ID
	}
	w.WriteHeader(http.StatusOK)
})

// 3. No regresso, ler a sessão só para mostrar o estado certo.
lida, err := cliente.GetCheckoutSession(ctx, r.URL.Query().Get("session_id"))
if err == nil && lida.Paid() { /* "pago" */ }
```

Em desenvolvimento, contra a API local:
`spolly.NewClient(chave, spolly.WithBaseURL("http://localhost:8084"))`.

## Estornar

```go
estorno, err := cliente.CreateRefund(ctx, &spolly.RefundParams{
	PaymentID: "pay_9Hq2Lm4Rt8Vx1Zb6Nc3Kd0",
	Amount:    500000, // zero estorna o que resta
	Reason:    "Devolução parcial a pedido do cliente.",
}, spolly.WithIdempotencyKey("pedido-1042-estorno-1"))
if errors.Is(err, &spolly.Error{Code: spolly.CodeRefundExceedsCharged}) {
	// nunca mais do que o que resta do pagamento
}
```

## Subscrições

```go
doze := 12
sessao, err := cliente.CreateCheckoutSession(ctx, &spolly.CheckoutSessionParams{
	Amount: 500000, Currency: spolly.AOA, Description: "Plano mensal",
	Kind:         spolly.KindSubscription,
	Subscription: &spolly.SubscriptionPlan{Interval: spolly.IntervalMonth, Cycles: &doze}, // Cycles nil: até cancelar
})

// Depois do primeiro pagamento, a sessão tem o id da subscrição (e chega o
// webhook subscription.created).
sub, err := cliente.GetSubscription(ctx, *lida.Subscription.ID)
link, err := cliente.CreateSubscriptionPortalLink(ctx, sub.ID) // mandar link.URL ao cliente
sub, err = cliente.CancelSubscription(ctx, sub.ID, nil)        // no fim do ciclo pago
```

## O que o cliente faz sozinho

- **Idempotência.** Todos os `POST` levam `Idempotency-Key`. Sem
  `WithIdempotencyKey`, gera-se um UUID v4 por chamada, o mesmo em todas as
  retentativas dessa chamada. Para ser idempotente entre chamadas (o mesmo
  pedido da loja depois de um reinício), passa-se uma chave própria.
- **Retentativas.** Falha de rede, 429, 500, 502, 503 e 504 repetem-se até
  `WithMaxRetries` vezes (2 por omissão), com espera exponencial e aleatória
  (0,5 s, 1 s, até 8 s), e com o `Retry-After` quando vem (até 30 s). Um 4xx
  nunca se repete.
- **Tempo limite.** 30 segundos por tentativa (`WithTimeout`); o `ctx` de
  quem chama manda sobre tudo.
- **Uma resposta que não se lê é um erro** (`ErrInvalidResponse`), e nunca uma
  sessão vazia. Um erro da API sem o corpo dela (um proxy pelo meio) é
  `*Error` com `Code` igual a `resposta_ilegivel`.

## Erros

```go
var e *spolly.Error
if errors.As(err, &e) {
	log.Printf("%d %s: %s %v", e.StatusCode, e.Code, e.Message, e.Fields)
}
```

`e.Code` é o código estável da API (`spolly.CodeInvalidData`,
`spolly.CodeInvalidKey`, `spolly.CodeSessionNotFound`, ...); `e.Message` é a
explicação em português, para mostrar; `e.Fields` tem uma mensagem por campo no
422 `dados_invalidos`.

## Verificar um webhook

- A tolerância do tempo é de 5 minutos por omissão (`Verifier.Tolerance`), e
  não se desliga.
- Durante a rotação do segredo no painel, o cabeçalho traz duas assinaturas, e
  basta uma bater; do lado de quem recebe, `Secrets` aceita os dois.
- As recusas são `*webhook.VerificationError`, e comparam-se com `errors.Is`:
  `ErrNoHeader`, `ErrMalformedHeader`, `ErrNoSignature`,
  `ErrSignatureMismatch`, `ErrTimestampOutsideTolerance`, `ErrInvalidPayload`,
  `ErrBodyTooLarge`, `ErrNoSecret`.

## Exemplos

- Os testes `Example` de cada operação (`go doc`, ou `example_test.go`).
- `examples/checkout`: o percurso inteiro. Confirma a chave, cria uma sessão,
  espera pelo webhook `payment.succeeded` dessa sessão (verificado com o SDK) e
  lê a sessão no fim.

  ```bash
  SPOLLY_SECRET_KEY=sk_test_... SPOLLY_WEBHOOK_SECRETS=whsec_... \
    go run ./examples/checkout -api http://localhost:8084 -porta 7400
  ```

  O endpoint, no painel, aponta para `http://127.0.0.1:7400/webhooks/spolly`.
- `examples/receber-webhooks`: só o servidor que recebe e verifica.

Para usar o módulo a partir de outro, antes de haver etiqueta, um `go.work`
**fora** do repositório (nunca um `replace` commitado):

```
go 1.26.0
use (
	.
	/caminho/para/spolly-payments/sdk-go
)
```

## Quem o usa primeiro

Os quatro produtos da casa, que são todos Go: o Saluto, o Spolly, o RotaLar e o
Aduela.
