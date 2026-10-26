# spolly-payments-go

O SDK em Go do [Spolly Payments](https://github.com/spolly-ao/spolly-payments):
criar e ler sessões de checkout, estornar, confirmar a conta da chave, gerir as
contas ligadas de uma plataforma, e verificar a assinatura dos webhooks. Só a biblioteca padrão, Go 1.26 ou mais
recente.

```bash
go get github.com/spolly-ao/spolly-payments-go@v0.9.0
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
| `spolly` | `(*Client).CreateCoupon(ctx, *CouponParams, ...RequestOption)` | `POST /v1/coupons` |
| `spolly` | `(*Client).ListCoupons(ctx, *ListCouponsParams)` | `GET /v1/coupons` (`limit`, `starting_after`, `code`) |
| `spolly` | `(*Client).GetCoupon(ctx, id)` | `GET /v1/coupons/{id}` |
| `spolly` | `(*Client).UpdateCoupon(ctx, id, *CouponUpdateParams)` | `PATCH /v1/coupons/{id}`: o nome, o limite, a data de fim, o ativo e os metadados |
| `spolly` | `Coupon`, `CouponList`, `Coupon*` (as durações), `Discount`, `SessionCoupon`, `SubscriptionDiscount` | Os cupões, e o desconto na sessão, no pagamento e na subscrição |
| `spolly` | `(*Client).WithAccount(mer_...)` | Uma cópia do cliente que pede em nome de uma conta ligada (cabeçalho `Spolly-Account`, `AccountHeader`) em todas as rotas de `/v1`, menos as `/v1/accounts` |
| `spolly` | `(*Client).CreateAccount(ctx, *AccountParams, ...RequestOption)` | `POST /v1/accounts` |
| `spolly` | `(*Client).ListAccounts(ctx, *ListAccountsParams)` | `GET /v1/accounts` (`limit`, `starting_after`, `reference`) |
| `spolly` | `(*Client).GetConnectedAccount(ctx, id)` | `GET /v1/accounts/{id}` |
| `spolly` | `(*Client).UpdateAccount(ctx, id, *AccountParams)` | `PATCH /v1/accounts/{id}` |
| `spolly` | `(*Client).UploadAccountDocument(ctx, id, *AccountDocumentParams, ...RequestOption)` | `POST /v1/accounts/{id}/documents` (multipart, `type` e `file`) |
| `spolly` | `(*Client).SetAccountBankAccount(ctx, id, *BankAccountParams)` | `PUT /v1/accounts/{id}/bank_account` |
| `spolly` | `(*Client).GetAccountBalance(ctx, id)` | `GET /v1/accounts/{id}/balance` |
| `spolly` | `(*Client).CreateAccountPayout(ctx, id, *PayoutParams, ...RequestOption)` | `POST /v1/accounts/{id}/payouts` |
| `spolly` | `(*Client).ListAccountPayouts(ctx, id)` | `GET /v1/accounts/{id}/payouts` |
| `spolly` | `(*Client).GetAccountPayout(ctx, id, payoutID)` | `GET /v1/accounts/{id}/payouts/{payout_id}` |
| `spolly` | `ConnectedAccount`, `AccountVerification`, `AccountBankAccount`, `AccountDocument`, `AccountList`, `Balance`, `Payout`, `PayoutList`, `Verification*`, `Missing*`, `Document*`, `BankAccount*`, `Payout*`, `MaxDocumentSize`, `ErrInvalidDocument` | As contas ligadas, o saldo e os saques |
| `spolly` | `WithIdempotencyKey(k)`, `NewIdempotencyKey()` | A `Idempotency-Key` de um `POST` |
| `spolly` | `*Error` (`StatusCode`, `Code`, `Message`, `Fields`, `IdempotencyKey`), `ErrorCode(err)`, `Code*` | As recusas da API |
| `spolly` | `ErrInvalidResponse`, `ErrNoKey` | Uma resposta que não se lê; chamar sem chave |
| `spolly` | `Money`, `AOA`, `USD`, `EUR`, `Method*` (com `MethodBankTransfer` e `MethodDirectDebit`, só em AOA e só quando pedidos), `Mode*`, `Session*`, `Refund*`, `Subscription*`, `Renewal*`, `Settlement*` (para onde foi o dinheiro de um pagamento: o saldo, ou a conta bancária do comerciante na transferência), `Kind*`, `Interval*` | Os tipos e os valores do contrato |
| `webhook` | `Verifier`, `Verify`, `ConstructEvent`, `(Verifier).ParseRequest`, `Sign` | A assinatura dos webhooks |
| `webhook` | `*VerificationError`, `Err*`, `Event` (com `Account` nos eventos de uma conta ligada), `Event*` | As recusas, o envelope e os tipos de evento |

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

Um cupão que vale só no primeiro ciclo: `FirstCycleAmount` no plano. O primeiro
pagamento cobra-o, e as renovações cobram o `Amount` (no cartão, o Stripe renova
sozinho pelo preço cheio). Um cancelamento no fim do ciclo dá dois
`subscription.cancelled`: o do pedido, com a subscrição ainda `active`, e o do
fim, com `status` `cancelled`; corta-se o acesso pelo segundo.

## Cupões

Um cupão é de percentagem (`PercentOff`) ou de valor fixo (`AmountOff` com
`Currency`, e só vale nessa moeda). Numa subscrição vale no primeiro ciclo
(`CouponOnce`), nos primeiros N (`CouponRepeating` com `DurationCycles`) ou em
todos (`CouponForever`). O desconto não muda depois de criado.

```go
cupao, err := cliente.CreateCoupon(ctx, &spolly.CouponParams{
	Code: "BEMVINDO10", PercentOff: 10, Duration: spolly.CouponRepeating, DurationCycles: 3,
	MaxRedemptions: 100,
})

// Aplicado já pela loja:
sessao, err := cliente.CreateCheckoutSession(ctx, &spolly.CheckoutSessionParams{
	Amount: 1500000, Currency: spolly.AOA, Description: "Plano mensal", Coupon: cupao.Code,
})
// Recusa: spolly.CodeCouponInvalid (422). sessao.Discount.Amount é quanto desconta,
// e o Amount da sessão continua a ser o preço antes dele.

// Ou escrito pelo comprador no checkout:
sessao, err = cliente.CreateCheckoutSession(ctx, &spolly.CheckoutSessionParams{
	Amount: 1500000, Currency: spolly.AOA, Description: "Vestido", AllowPromotionCodes: true,
})

// Desativar: deixa de se aplicar, e quem já o tem fica com ele.
nao := false
_, err = cliente.UpdateCoupon(ctx, cupao.ID, &spolly.CouponUpdateParams{Active: &nao})
```

O `Payment` do `payment.succeeded` traz o `Amount` já descontado, a `Fee` sobre
ele e o `Discount`; a `Subscription` traz o `Discount` com os ciclos que cobre.
Um cupão não vai com o `FirstCycleAmount` de um plano.

## Comissão de plataforma

Uma plataforma que vende em nome de um comerciante cria a sessão com a chave
dele e fica com uma comissão no próprio pagamento. O comerciante tem de a ter
autorizado no painel, com um máximo.

```go
hotel := spolly.NewClient(os.Getenv("CHAVE_DO_HOTEL"))
sessao, err := hotel.CreateCheckoutSession(ctx, &spolly.CheckoutSessionParams{
	Amount: 1500000, Currency: spolly.AOA, Description: "Duas noites",
	PlatformFee: &spolly.PlatformFee{Platform: "mer_38IAs7qmknSwC1F9XYtR1b", Amount: 150000},
})
// Recusas: spolly.CodePlatformNotAuthorized (403),
// CodePlatformFeeAboveMaximum e CodePlatformFeeInvalid (422).

// No payment.succeeded, o Payment diz o que a plataforma recebeu.
var p spolly.Payment
_ = json.Unmarshal(evento.Data.Object, &p) // p.PlatformFee, p.Net
```

## A transferência bancária vai para a conta do comerciante

Com `spolly.MethodBankTransfer` (só em AOA, e só pedido por nome), o comprador
transfere direto para a conta bancária do comerciante, que o confirma no painel.
O `payment.succeeded` diz para onde foi o dinheiro:

```go
var p spolly.Payment
_ = json.Unmarshal(evento.Data.Object, &p)
if p.Settlement == spolly.SettlementMerchantAccount {
	// O Amount entrou na conta bancária do comerciante; a Fee sai do saldo dele.
}
```

Uma sessão com `PlatformFee` não pode pedir a transferência
(`CodePlatformFeeInvalid`), e um pagamento por transferência não se estorna pela
API (`CodePaidToMerchantAccount`, 409): a devolução faz-se da conta do
comerciante.

## Um pagamento em análise

O antifraude da Spolly pode reter um pagamento depois de confirmado: o
`payment.succeeded` chega na mesma, mas o dinheiro fica no pendente até a
equipa o libertar. Chegam mais dois eventos, com o mesmo `Payment` no objeto:

```go
switch evento.Type {
case webhook.EventPaymentHeld:
	// Retido: entrega a encomenda se quiseres, mas o dinheiro ainda não sai.
case webhook.EventPaymentReleased:
	// Libertado: segue para o disponível na libertação seguinte.
}
```

Se a equipa confirmar a fraude, o pagamento é estornado ao comprador e chega o
`refund.created`.

## Contas ligadas

Uma plataforma habilitada pela equipa do Spolly Payments cria e gere pela API os
vendedores que vendem por ela, e o vendedor nunca fala com o Spolly Payments.
Sem a habilitação, estas operações dão `spolly.CodeConnectedAccountsNotEnabled`
(403).

```go
plataforma := spolly.NewClient(os.Getenv("SPOLLY_SECRET_KEY"))

// 1. Criar o vendedor, com a referência dele do lado da plataforma.
conta, err := plataforma.CreateAccount(ctx, &spolly.AccountParams{
	Name: "Kizomba Eventos", LegalName: "Joana Manuel", Reference: "vendedor_481",
})

// 2. A verificação: o documento de identificação, o comprovativo do IBAN, e a
// conta bancária. Com tudo entregue, segue sozinha para análise (em teste,
// aprova-se sozinha). conta.Verification.Missing diz o que falta.
bi, _ := os.Open("bi.pdf")
_, err = plataforma.UploadAccountDocument(ctx, conta.ID, &spolly.AccountDocumentParams{
	Type: spolly.DocumentIdentity, FileName: "bi.pdf", Content: bi,
})
iban, _ := os.Open("iban.pdf")
comprovativo, err := plataforma.UploadAccountDocument(ctx, conta.ID, &spolly.AccountDocumentParams{
	Type: spolly.DocumentBankAccountProof, FileName: "iban.pdf", Content: iban,
})
conta, err = plataforma.SetAccountBankAccount(ctx, conta.ID, &spolly.BankAccountParams{
	IBAN: "AO06004000010123456789012", Holder: "Joana Manuel", ProofDocument: comprovativo.ID,
})

// 3. Cobrar em nome do vendedor: a cópia manda Spolly-Account em cada pedido.
// Com Platform vazio, a comissão é da plataforma.
vendedor := plataforma.WithAccount(conta.ID)
sessao, err := vendedor.CreateCheckoutSession(ctx, &spolly.CheckoutSessionParams{
	Amount: 1500000, Currency: spolly.AOA, Description: "Bilhete",
	PlatformFee: &spolly.PlatformFee{Amount: 150000},
})

// 4. O saldo (com sinal: pode ser negativo) e os saques.
saldo, err := plataforma.GetAccountBalance(ctx, conta.ID)
saque, err := plataforma.CreateAccountPayout(ctx, conta.ID, &spolly.PayoutParams{Amount: 2500000},
	spolly.WithIdempotencyKey("saq_9921"))
```

- `GetAccount` continua a ser a conta da chave (`GET /v1/conta`); a conta ligada
  lê-se com `GetConnectedAccount`, e o tipo dela é `ConnectedAccount`.
- As rotas `/v1/accounts` nunca levam o `Spolly-Account`, mesmo chamadas a partir
  da cópia de `WithAccount`: a conta vai no caminho.
- Os eventos da conta chegam aos webhooks da plataforma com `evento.Account`
  preenchido (`account.updated`, `balance.available`, `payout.created`,
  `payout.processing`, `payout.paid`, `payout.failed`, e os de pagamento das
  sessões feitas em nome dela). Vazio, o evento é da própria plataforma.
- Um saque: `PayoutPending` (pedido), `PayoutProcessing` (aprovado),
  `PayoutPaid` ou `PayoutFailed` (o valor volta ao disponível). Nunca mais do
  que o disponível (`CodeInsufficientBalance`).

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
