package spolly

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// RefundParams é o pedido de um estorno.
type RefundParams struct {
	// PaymentID é o pagamento a estornar (pay_...), do payment.succeeded ou do
	// Payment da sessão.
	PaymentID string `json:"payment_id"`
	// Amount é quanto devolver, em unidades menores, na moeda do pagamento.
	// Zero estorna o que resta.
	Amount int64 `json:"amount,omitempty"`
	// Reason volta no estorno e no webhook refund.created.
	Reason string `json:"reason,omitempty"`
}

// Os estados de um estorno.
const (
	RefundProcessing = "processing"
	RefundPending    = "pending"
	RefundSucceeded  = "succeeded"
	RefundFailed     = "failed"
)

// Refund é um estorno.
type Refund struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	PaymentID string `json:"payment_id"`
	Amount    Money  `json:"amount"`
	// Status é RefundProcessing (sem resposta do gateway ainda),
	// RefundPending (à espera da transferência), RefundSucceeded ou
	// RefundFailed (o gateway recusou, e o saldo voltou ao que era).
	Status string `json:"status"`
	Reason string `json:"reason"`
	// FailureReason só nos RefundFailed.
	FailureReason string    `json:"failure_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (r *Refund) valido() bool {
	return strings.HasPrefix(r.ID, "re_") && r.Object == "refund"
}

// CreateRefund estorna um pagamento, todo ou em parte (POST /v1/refunds).
// Nunca mais do que o que resta: acima disso, *Error com
// CodeRefundExceedsCharged.
func (c *Client) CreateRefund(ctx context.Context, p *RefundParams, opts ...RequestOption) (*Refund, error) {
	var r Refund
	if err := c.pedir(ctx, http.MethodPost, "/v1/refunds", p, &r, opts); err != nil {
		return nil, err
	}
	return &r, nil
}

// GetRefund lê um estorno (GET /v1/refunds/{id}): é o que diz o desfecho de
// um que ficou RefundProcessing ou RefundPending.
func (c *Client) GetRefund(ctx context.Context, id string) (*Refund, error) {
	var r Refund
	if err := c.pedir(ctx, http.MethodGet, caminhoCom("/v1/refunds", id), nil, &r, nil); err != nil {
		return nil, err
	}
	return &r, nil
}
