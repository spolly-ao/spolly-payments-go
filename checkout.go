package spolly

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Currency é a moeda. USD e EUR só pelo cartão; BRL pelo cartão e pelo Pix.
type Currency string

// As moedas.
const (
	AOA Currency = "AOA"
	USD Currency = "USD"
	EUR Currency = "EUR"
	BRL Currency = "BRL"
)

// PaymentMethod é um método de pagamento do checkout.
type PaymentMethod string

// Os métodos: Multicaixa Express, referência, BAI Paga, cartão, Pix (só em BRL),
// transferência bancária e débito direto. Os dois últimos existem só em AOA e
// só se oferecem quando a sessão os pede por nome em PaymentMethods: uma lista
// vazia não os inclui.
const (
	MethodMCX       PaymentMethod = "mcx"
	MethodReference PaymentMethod = "reference"
	MethodBAIPaga   PaymentMethod = "baipaga"
	MethodCard      PaymentMethod = "card"
	MethodPix       PaymentMethod = "pix"
	// MethodBankTransfer é a transferência bancária: o comprador transfere
	// direto para a conta bancária do comerciante, com um descritivo único, e
	// o pagamento fica pendente até 7 dias, até o comerciante o confirmar no
	// painel (Payment.Settlement é SettlementMerchantAccount).
	MethodBankTransfer PaymentMethod = "bank_transfer"
	// MethodDirectDebit é o débito direto: o comprador autoriza um mandato no
	// banco dele, e a cobrança apresenta-se contra o mandato.
	MethodDirectDebit PaymentMethod = "direct_debit"
)

// Mode é o modo de uma chave e de tudo o que ela cria.
type Mode string

// Os modos.
const (
	ModeTest Mode = "test"
	ModeLive Mode = "live"
)

// Money é dinheiro em unidades menores inteiras: 15.000,00 Kz é
// Money{Amount: 1500000, Currency: AOA}.
type Money struct {
	Amount   int64    `json:"amount"`
	Currency Currency `json:"currency"`
}

// LineItem é uma linha do que se paga. As linhas têm de somar o Amount da
// sessão.
type LineItem struct {
	Description string `json:"description"`
	// UnitAmount é o preço unitário em unidades menores (em AOA, kwanzas
	// inteiros).
	UnitAmount int64 `json:"unit_amount"`
	Quantity   int64 `json:"quantity"`
}

// Customer são os dados do comprador que se conhecem à partida.
type Customer struct {
	// Email recebe a referência e o recibo.
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
	// Phone é um telemóvel angolano; a API guarda-o como 244XXXXXXXXX.
	Phone string `json:"phone,omitempty"`
}

// CheckoutSessionParams é o pedido de uma sessão de checkout.
type CheckoutSessionParams struct {
	// Amount em unidades menores inteiras. Em AOA, múltiplo de 100.
	Amount   int64    `json:"amount"`
	Currency Currency `json:"currency"`
	// Description é o que o comprador vê que está a pagar. Obrigatória sem
	// LineItems.
	Description string     `json:"description,omitempty"`
	LineItems   []LineItem `json:"line_items,omitempty"`
	// PaymentMethods vazio aceita todos os que têm gateway na moeda e no modo.
	PaymentMethods []PaymentMethod `json:"payment_methods,omitempty"`
	// SuccessURL recebe o comprador depois de pagar, com session_id (ou no
	// lugar de {CHECKOUT_SESSION_ID}). O regresso não prova pagamento:
	// confirma-se pelo webhook payment.succeeded, ou por GetCheckoutSession.
	SuccessURL string `json:"success_url,omitempty"`
	// CancelURL recebe o comprador que desistiu.
	CancelURL string `json:"cancel_url,omitempty"`
	// Metadata volta tal como foi, na sessão e nos eventos.
	Metadata map[string]string `json:"metadata,omitempty"`
	Customer *Customer         `json:"customer,omitempty"`
	// ExpiresIn é a vida da sessão em segundos, de 900 a 86400. Zero é 24
	// horas.
	ExpiresIn int64 `json:"expires_in,omitempty"`
	// Kind é KindPayment (por omissão, vazio) ou KindSubscription.
	Kind string `json:"kind,omitempty"`
	// Subscription é o plano de uma sessão KindSubscription: obrigatório nela,
	// e recusado nas outras. O preço de cada ciclo é o Amount da sessão.
	Subscription *SubscriptionPlan `json:"subscription,omitempty"`
	// PlatformFee é a comissão de uma plataforma que vende em nome do
	// comerciante da chave: o comerciante tem de a ter autorizado no painel,
	// e o Amount nunca passa o máximo dele. Numa sessão KindSubscription,
	// repete-se em cada renovação. nil sem comissão.
	PlatformFee *PlatformFee `json:"platform_fee,omitempty"`
}

// PlatformFee é a comissão de uma plataforma num pagamento: a plataforma que
// a recebe (o identificador de comerciante dela, mer_...) e o valor, em
// unidades menores da moeda da sessão (em AOA, múltiplo de 100).
type PlatformFee struct {
	// Platform é o mer_ da plataforma. Vazio só num cliente de WithAccount:
	// aí a plataforma é a da chave (contas ligadas).
	Platform string `json:"platform,omitempty"`
	Amount   int64  `json:"amount"`
}

// Os motivos de uma comissão pedida que não se aplicou a um pagamento
// (Payment.PlatformFeeSkipped): o pagamento entrou sem comissão.
const (
	// PlatformFeeAuthorizationRevoked: o comerciante revogou a autorização.
	PlatformFeeAuthorizationRevoked = "authorization_revoked"
	// PlatformFeeAboveMaximum: o máximo autorizado já não cobre a comissão.
	PlatformFeeAboveMaximum = "above_maximum"
	// PlatformFeeExceedsNet: a comissão já não cabe no líquido do comerciante.
	PlatformFeeExceedsNet = "exceeds_net"
)

// Settlement diz para onde foi o dinheiro de um pagamento.
type Settlement string

const (
	// SettlementBalance: o dinheiro entrou no saldo do comerciante no Spolly
	// Payments (todos os métodos menos a transferência bancária).
	SettlementBalance Settlement = "balance"
	// SettlementMerchantAccount: o dinheiro entrou direto na conta bancária do
	// comerciante (a transferência bancária), que o confirma no painel; a taxa
	// sai do saldo dele.
	SettlementMerchantAccount Settlement = "merchant_account"
)

// Payment é o objeto de um pagamento nos eventos payment.succeeded e
// payment.failed (o Data.Object do webhook.Event). Lê-se com
// json.Unmarshal(evento.Data.Object, &p).
type Payment struct {
	ID                string `json:"id"`
	Object            string `json:"object"`
	CheckoutSessionID string `json:"checkout_session_id"`
	// Status é "succeeded" ou "failed".
	Status string `json:"status"`
	Amount Money  `json:"amount"`
	// Fee é a taxa do Spolly Payments.
	Fee Money `json:"fee"`
	// Net é o que fica ao comerciante: Amount menos Fee e menos a comissão
	// da plataforma, quando a há. Com Settlement SettlementBalance, é o que
	// entra no saldo; com SettlementMerchantAccount, o comerciante recebeu o
	// Amount na conta dele e a Fee sai do saldo.
	Net Money `json:"net"`
	// Settlement diz para onde foi o dinheiro: SettlementBalance ou
	// SettlementMerchantAccount.
	Settlement Settlement    `json:"settlement"`
	Method     PaymentMethod `json:"method"`
	// FailureReason diz porque falhou, só nos falhados.
	FailureReason string            `json:"failure_reason,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	Metadata      map[string]string `json:"metadata"`
	// PaidAfterExpiry diz que o pagamento entrou depois do prazo da sessão.
	PaidAfterExpiry bool `json:"paid_after_expiry,omitempty"`
	// PlatformFee é a comissão que a plataforma recebeu neste pagamento, ou
	// nil.
	PlatformFee *PlatformFee `json:"platform_fee"`
	// PlatformFeeSkipped diz porque uma comissão pedida não se aplicou (um
	// dos PlatformFee*), ou vazio.
	PlatformFeeSkipped string `json:"platform_fee_skipped,omitempty"`
	// FeeOnTop é a parte do Amount que o comprador pagou por cima, quando o
	// comerciante lhe passa a taxa (no painel, em Aplicações): é igual à Fee,
	// e o Net é o valor da cobrança. nil quando a taxa sai do valor.
	FeeOnTop *Money `json:"fee_on_top"`
}

// Os tipos de sessão. Chama-se kind, e não mode, porque o Mode é o modo da
// chave (teste ou produção).
const (
	KindPayment      = "payment"
	KindSubscription = "subscription"
	// KindRenewal é a sessão que a Spolly cria para pagar um ciclo de uma
	// subscrição; não se cria pela API.
	KindRenewal = "renewal"
)

// Os intervalos de uma subscrição.
const (
	IntervalWeek     = "week"
	IntervalMonth    = "month"
	IntervalQuarter  = "quarter"
	IntervalHalfYear = "half_year"
	IntervalYear     = "year"
)

// SubscriptionPlan é o que uma sessão de subscrição contrata além do preço.
type SubscriptionPlan struct {
	// Interval é de quanto em quanto tempo se renova (um dos Interval*).
	Interval string `json:"interval"`
	// Cycles é quantas vezes se cobra, no máximo 1000; nil é até alguém
	// cancelar.
	Cycles *int `json:"cycles,omitempty"`
	// FirstCycleAmount é o valor do primeiro ciclo, quando é mais barato do
	// que o Amount da sessão (um cupão que vale só no primeiro mês): o
	// primeiro pagamento cobra-o, e as renovações cobram o Amount. Zero, todos
	// os ciclos custam o Amount. Em AOA, múltiplo de 100.
	FirstCycleAmount int64 `json:"first_cycle_amount,omitempty"`
}

// SessionSubscription é o campo subscription de uma sessão de subscrição ou
// de renovação.
type SessionSubscription struct {
	// ID é a subscrição (sub_...), ou nil numa sessão de subscrição ainda
	// por pagar: a subscrição nasce com o pagamento.
	ID       *string `json:"id"`
	Interval string  `json:"interval"`
	Cycles   *int    `json:"cycles"`
	// Cycle é o ciclo que uma sessão de renovação paga; nil nas outras.
	Cycle *int `json:"cycle"`
	// FirstCycleAmount é o valor do primeiro ciclo, quando é mais barato do
	// que o Amount, ou nil.
	FirstCycleAmount *int64 `json:"first_cycle_amount"`
}

// Os estados de uma sessão.
const (
	SessionOpen     = "open"
	SessionComplete = "complete"
	SessionExpired  = "expired"
)

// CheckoutSession é uma sessão de checkout.
type CheckoutSession struct {
	// ID (cs_test_... ou cs_live_...) é a capacidade do comprador para abrir
	// o checkout: não se publica.
	ID     string `json:"id"`
	Object string `json:"object"`
	Mode   Mode   `json:"mode"`
	// Status é SessionOpen, SessionComplete ou SessionExpired.
	Status string `json:"status"`
	// URL é o checkout alojado, para onde se manda o comprador.
	URL            string            `json:"url"`
	Amount         Money             `json:"amount"`
	Description    string            `json:"description"`
	LineItems      []LineItem        `json:"line_items"`
	PaymentMethods []PaymentMethod   `json:"payment_methods"`
	SuccessURL     *string           `json:"success_url"`
	CancelURL      *string           `json:"cancel_url"`
	Metadata       map[string]string `json:"metadata"`
	Customer       Customer          `json:"customer"`
	ExpiresAt      time.Time         `json:"expires_at"`
	CreatedAt      time.Time         `json:"created_at"`
	CompletedAt    *time.Time        `json:"completed_at"`
	// Payment é o pagamento que completou a sessão, ou nil.
	Payment *SessionPayment `json:"payment"`
	// Kind é KindPayment, KindSubscription ou KindRenewal.
	Kind string `json:"kind"`
	// Subscription é o plano e a subscrição, numa sessão de subscrição ou de
	// renovação; nil numa de pagamento.
	Subscription *SessionSubscription `json:"subscription"`
	// Application é a aplicação do comerciante de onde a sessão vem (app_...):
	// a da chave que a criou, a do link, ou a da subscrição numa renovação.
	// nil sem aplicação. Os eventos dela levam-na em "application".
	Application *string `json:"application"`
	// PlatformFee é a comissão da plataforma que a sessão pede, ou nil.
	PlatformFee *PlatformFee `json:"platform_fee"`
	// FeeOnTop diz que o comprador paga a taxa do Spolly Payments por cima do
	// Amount: a escolha da aplicação da chave, ou a global do comerciante,
	// congelada quando a sessão nasceu. O comerciante fica com o Amount.
	FeeOnTop bool `json:"fee_on_top"`
}

func (s *CheckoutSession) valido() bool {
	return strings.HasPrefix(s.ID, "cs_") && s.Object == "checkout_session"
}

// Paid diz se a sessão está paga: completa, e com um pagamento confirmado.
func (s *CheckoutSession) Paid() bool {
	return s.Status == SessionComplete && s.Payment != nil && s.Payment.Status == "succeeded"
}

// SessionPayment é o resumo do pagamento de uma sessão completa.
type SessionPayment struct {
	ID     string        `json:"id"`
	Status string        `json:"status"`
	Method PaymentMethod `json:"method"`
	// Amount é o que o comprador pagou; FeeOnTop, a parte dele que foi a
	// taxa paga por cima, ou nil.
	Amount   Money  `json:"amount"`
	FeeOnTop *Money `json:"fee_on_top"`
}

// CreateCheckoutSession cria uma sessão (POST /v1/checkout/sessions) e
// devolve-a com o URL do checkout alojado.
func (c *Client) CreateCheckoutSession(ctx context.Context, p *CheckoutSessionParams, opts ...RequestOption) (*CheckoutSession, error) {
	var s CheckoutSession
	if err := c.pedir(ctx, http.MethodPost, "/v1/checkout/sessions", p, &s, opts); err != nil {
		return nil, err
	}
	return &s, nil
}

// GetCheckoutSession lê uma sessão (GET /v1/checkout/sessions/{id}). É o que
// se chama, do lado do servidor, no regresso do comprador.
func (c *Client) GetCheckoutSession(ctx context.Context, id string) (*CheckoutSession, error) {
	var s CheckoutSession
	if err := c.pedir(ctx, http.MethodGet, caminhoCom("/v1/checkout/sessions", id), nil, &s, nil); err != nil {
		return nil, err
	}
	return &s, nil
}
