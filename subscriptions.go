package spolly

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Os estados de uma subscrição.
const (
	// SubscriptionActive: em vigor.
	SubscriptionActive = "active"
	// SubscriptionPastDue: o ciclo acabou sem o seguinte pago, e a tolerância
	// corre (o acesso mantém-se).
	SubscriptionPastDue = "past_due"
	// SubscriptionCancelled: terminada a pedido.
	SubscriptionCancelled = "cancelled"
	// SubscriptionExpired: terminada sem pedido, por falta de pagamento ou
	// porque os ciclos contratados acabaram (EndReason diz qual).
	SubscriptionExpired = "expired"
)

// Renewal diz como renova o ciclo seguinte de uma subscrição.
type Renewal string

// Os modos de renovação.
const (
	// RenewalAutomatic: o ciclo cobra-se sozinho, sem o cliente agir (o cartão
	// e o débito direto). Se a cobrança falhar, a subscrição passa a past_due
	// e o cliente recebe o aviso com o link do ciclo.
	RenewalAutomatic Renewal = "automatic"
	// RenewalNotice: o cliente recebe o aviso com o link do ciclo e paga
	// (Multicaixa Express, referência, BAI Paga, Pix, transferência).
	RenewalNotice Renewal = "notice"
)

// Subscription é uma subscrição. Nasce do primeiro pagamento de uma sessão
// KindSubscription; cada ciclo seguinte é um pagamento normal, com o seu
// payment.succeeded.
type Subscription struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	// Status é um dos Subscription*.
	Status string `json:"status"`
	// Amount é o preço de cada ciclo.
	Amount   Money  `json:"amount"`
	Interval string `json:"interval"`
	// Cycles são os ciclos contratados; nil renova até ser cancelada.
	Cycles *int `json:"cycles"`
	// Cycle é o ciclo em vigor. O primeiro é o 1.
	Cycle              int       `json:"cycle"`
	CurrentPeriodStart time.Time `json:"current_period_start"`
	// CurrentPeriodEnd é até quando o ciclo em vigor está pago.
	CurrentPeriodEnd time.Time `json:"current_period_end"`
	// Method é o método do próximo ciclo.
	Method PaymentMethod `json:"method"`
	// Renewal diz como renova o ciclo seguinte: RenewalAutomatic ou
	// RenewalNotice.
	Renewal Renewal `json:"renewal"`
	// CancelAtPeriodEnd: o cancelamento foi pedido para o fim do ciclo; a
	// subscrição continua active até CurrentPeriodEnd e não renova.
	CancelAtPeriodEnd bool       `json:"cancel_at_period_end"`
	CancelledAt       *time.Time `json:"cancelled_at,omitempty"`
	// CancelledBy é "customer" ou "merchant".
	CancelledBy *string    `json:"cancelled_by,omitempty"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	// EndReason é "requested", "unpaid" ou "completed".
	EndReason         *string           `json:"end_reason,omitempty"`
	CheckoutSessionID string            `json:"checkout_session_id"`
	LatestPaymentID   string            `json:"latest_payment_id,omitempty"`
	Customer          Customer          `json:"customer"`
	Metadata          map[string]string `json:"metadata"`
}

func (s *Subscription) valido() bool {
	return strings.HasPrefix(s.ID, "sub_") && s.Object == "subscription"
}

// CancelSubscriptionParams diz como cancelar.
type CancelSubscriptionParams struct {
	// AtPeriodEnd: nil ou true cancela no fim do ciclo pago (a subscrição
	// continua até lá e não renova); false cancela já.
	AtPeriodEnd *bool `json:"at_period_end,omitempty"`
}

// SubscriptionPortalLink é um link de uso único para a página do cliente,
// onde ele vê a subscrição, muda o método e cancela.
type SubscriptionPortalLink struct {
	Object       string    `json:"object"`
	Subscription string    `json:"subscription"`
	URL          string    `json:"url"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (l *SubscriptionPortalLink) valido() bool {
	return l.Object == "subscription_portal" && l.URL != ""
}

// GetSubscription lê uma subscrição (GET /v1/subscriptions/{id}).
func (c *Client) GetSubscription(ctx context.Context, id string) (*Subscription, error) {
	var s Subscription
	if err := c.pedir(ctx, http.MethodGet, caminhoCom("/v1/subscriptions", id), nil, &s, nil); err != nil {
		return nil, err
	}
	return &s, nil
}

// CancelSubscription cancela uma subscrição (POST
// /v1/subscriptions/{id}/cancel), no fim do ciclo por omissão. Uma que já
// terminou é *Error com CodeSubscriptionEnded.
func (c *Client) CancelSubscription(ctx context.Context, id string, p *CancelSubscriptionParams, opts ...RequestOption) (*Subscription, error) {
	if p == nil {
		p = &CancelSubscriptionParams{}
	}
	var s Subscription
	if err := c.pedir(ctx, http.MethodPost, caminhoCom("/v1/subscriptions", id)+"/cancel", p, &s, opts); err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateSubscriptionPortalLink devolve um link de uso único para a página do
// cliente desta subscrição (POST /v1/subscriptions/{id}/portal). Manda-se ao
// cliente, e não se guarda: expira, e só abre uma vez.
func (c *Client) CreateSubscriptionPortalLink(ctx context.Context, id string, opts ...RequestOption) (*SubscriptionPortalLink, error) {
	var l SubscriptionPortalLink
	if err := c.pedir(ctx, http.MethodPost, caminhoCom("/v1/subscriptions", id)+"/portal", nil, &l, opts); err != nil {
		return nil, err
	}
	return &l, nil
}
