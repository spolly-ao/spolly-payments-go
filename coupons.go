package spolly

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// As durações de um cupão. Num pagamento único, um cupão vale uma vez,
// qualquer que seja a duração.
const (
	// CouponOnce vale no primeiro ciclo de uma subscrição.
	CouponOnce = "once"
	// CouponRepeating vale nos primeiros DurationCycles ciclos.
	CouponRepeating = "repeating"
	// CouponForever vale em todos os ciclos.
	CouponForever = "forever"
)

// CouponParams é o pedido de um cupão (POST /v1/coupons). Um cupão é de
// percentagem (PercentOff) ou de valor fixo (AmountOff com Currency, e só vale
// nessa moeda). O desconto não muda depois de criado.
type CouponParams struct {
	// Code é o que o comprador escreve: letras, números, - e _, de 3 a 40.
	// Guarda-se em maiúsculas, e é único por comerciante e por modo.
	Code string `json:"code"`
	Name string `json:"name,omitempty"`
	// PercentOff é a percentagem, de 1 a 99.
	PercentOff int `json:"percent_off,omitempty"`
	// AmountOff é o valor fixo em unidades menores (em AOA, múltiplo de 100).
	AmountOff int64    `json:"amount_off,omitempty"`
	Currency  Currency `json:"currency,omitempty"`
	// Duration é CouponOnce, CouponRepeating ou CouponForever.
	Duration string `json:"duration"`
	// DurationCycles é obrigatório no CouponRepeating, e só nele: de 2 a 1000.
	DurationCycles int `json:"duration_cycles,omitempty"`
	// MaxRedemptions é o limite de usos; zero não tem limite.
	MaxRedemptions int `json:"max_redemptions,omitempty"`
	// ExpiresAt é até quando se aplica, no futuro; nil não acaba.
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// CouponUpdateParams é a alteração de um cupão (PATCH /v1/coupons/{id}): só
// muda o que não for nil. O desconto não se muda.
type CouponUpdateParams struct {
	Name *string `json:"name,omitempty"`
	// MaxRedemptions novo; nunca abaixo de TimesRedeemed. Para tirar o
	// limite, RemoveMaxRedemptions.
	MaxRedemptions *int `json:"-"`
	// RemoveMaxRedemptions tira o limite (manda null).
	RemoveMaxRedemptions bool `json:"-"`
	// ExpiresAt nova. Para tirar a data, RemoveExpiresAt.
	ExpiresAt *time.Time `json:"-"`
	// RemoveExpiresAt tira a data de fim (manda null).
	RemoveExpiresAt bool `json:"-"`
	// Active false desativa: deixa de se poder aplicar, e as sessões e as
	// subscrições que já o têm ficam com ele.
	Active   *bool             `json:"active,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// corpo é o JSON do pedido, com null onde se tira o limite ou a data.
func (p *CouponUpdateParams) corpo() map[string]any {
	m := map[string]any{}
	if p.Name != nil {
		m["name"] = *p.Name
	}
	switch {
	case p.RemoveMaxRedemptions:
		m["max_redemptions"] = nil
	case p.MaxRedemptions != nil:
		m["max_redemptions"] = *p.MaxRedemptions
	}
	switch {
	case p.RemoveExpiresAt:
		m["expires_at"] = nil
	case p.ExpiresAt != nil:
		m["expires_at"] = p.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if p.Active != nil {
		m["active"] = *p.Active
	}
	if p.Metadata != nil {
		m["metadata"] = p.Metadata
	}
	return m
}

// Coupon é um cupão.
type Coupon struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	Mode   Mode   `json:"mode"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	// PercentOff é a percentagem, ou nil num de valor fixo.
	PercentOff *int `json:"percent_off"`
	// AmountOff é o valor fixo, na moeda em que vale, ou nil num de
	// percentagem.
	AmountOff *Money `json:"amount_off"`
	// Duration é CouponOnce, CouponRepeating ou CouponForever.
	Duration string `json:"duration"`
	// DurationCycles só no CouponRepeating.
	DurationCycles *int `json:"duration_cycles"`
	MaxRedemptions *int `json:"max_redemptions"`
	// TimesRedeemed são as sessões pagas com o cupão. O limite conta também
	// as abertas que ainda se podem pagar.
	TimesRedeemed int        `json:"times_redeemed"`
	ExpiresAt     *time.Time `json:"expires_at"`
	Active        bool       `json:"active"`
	// Valid diz que se pode aplicar agora: ativo, dentro da data, e com usos
	// por gastar.
	Valid     bool              `json:"valid"`
	Metadata  map[string]string `json:"metadata"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

func (c *Coupon) valido() bool {
	return strings.HasPrefix(c.ID, "cpn_") && c.Object == "coupon"
}

// ListCouponsParams filtra e pagina ListCoupons. nil é a primeira página, com
// 20 cupões.
type ListCouponsParams struct {
	// Limit é quantos cupões por página, de 1 a 100. Zero é 20.
	Limit int
	// StartingAfter é o ID do último cupão da página anterior.
	StartingAfter string
	// Code devolve só o cupão com este código (sem distinguir maiúsculas).
	Code string
}

// CouponList é uma página de cupões, do mais recente para o mais antigo.
type CouponList struct {
	Object string   `json:"object"`
	Data   []Coupon `json:"data"`
	// HasMore diz que há mais: pede-se a página seguinte com StartingAfter
	// igual ao ID do último de Data.
	HasMore bool `json:"has_more"`
}

func (l *CouponList) valido() bool {
	if l.Object != "list" || l.Data == nil {
		return false
	}
	for i := range l.Data {
		if !l.Data[i].valido() {
			return false
		}
	}
	return true
}

// SessionCoupon é o cupão como estava quando se aplicou a uma sessão ou a um
// pagamento.
type SessionCoupon struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	PercentOff     *int   `json:"percent_off"`
	AmountOff      *Money `json:"amount_off"`
	Duration       string `json:"duration"`
	DurationCycles *int   `json:"duration_cycles"`
}

// Discount é o desconto de um cupão numa sessão ou num pagamento: o cupão e
// quanto desconta. Numa sessão, o Amount dela é o preço antes do desconto; o
// Amount de um pagamento é já o valor depois dele, e a taxa é sobre esse.
type Discount struct {
	Coupon SessionCoupon `json:"coupon"`
	Amount Money         `json:"amount"`
}

// SubscriptionDiscount é o cupão de uma subscrição: o desconto de cada ciclo
// que ele cobre, e quantos ciclos cobre. O ciclo n paga Amount menos
// Discount.Amount enquanto n for menor ou igual a Cycles; Cycles nil quer
// dizer todos.
type SubscriptionDiscount struct {
	Coupon struct {
		ID   string `json:"id"`
		Code string `json:"code"`
	} `json:"coupon"`
	Amount Money `json:"amount"`
	Cycles *int  `json:"cycles"`
}

// CreateCoupon cria um cupão (POST /v1/coupons). Um código que já existe neste
// modo é *Error com CodeCouponCodeRepeated.
func (c *Client) CreateCoupon(ctx context.Context, p *CouponParams, opts ...RequestOption) (*Coupon, error) {
	var cp Coupon
	if err := c.pedir(ctx, http.MethodPost, "/v1/coupons", p, &cp, opts); err != nil {
		return nil, err
	}
	return &cp, nil
}

// ListCoupons lista os cupões (GET /v1/coupons), do mais recente para o mais
// antigo, no modo da chave.
func (c *Client) ListCoupons(ctx context.Context, p *ListCouponsParams) (*CouponList, error) {
	caminho := "/v1/coupons"
	if p != nil {
		q := url.Values{}
		if p.Limit > 0 {
			q.Set("limit", strconv.Itoa(p.Limit))
		}
		if p.StartingAfter != "" {
			q.Set("starting_after", p.StartingAfter)
		}
		if p.Code != "" {
			q.Set("code", p.Code)
		}
		if len(q) > 0 {
			caminho += "?" + q.Encode()
		}
	}
	var l CouponList
	if err := c.pedir(ctx, http.MethodGet, caminho, nil, &l, nil); err != nil {
		return nil, err
	}
	return &l, nil
}

// GetCoupon lê um cupão (GET /v1/coupons/{id}). Um que não é deste comerciante,
// ou deste modo, é *Error com CodeCouponNotFound.
func (c *Client) GetCoupon(ctx context.Context, id string) (*Coupon, error) {
	var cp Coupon
	if err := c.pedir(ctx, http.MethodGet, caminhoCom("/v1/coupons", id), nil, &cp, nil); err != nil {
		return nil, err
	}
	return &cp, nil
}

// UpdateCoupon muda o nome, o limite, a data de fim, o ativo ou os metadados de
// um cupão (PATCH /v1/coupons/{id}).
func (c *Client) UpdateCoupon(ctx context.Context, id string, p *CouponUpdateParams) (*Coupon, error) {
	if p == nil {
		p = &CouponUpdateParams{}
	}
	var cp Coupon
	if err := c.pedir(ctx, http.MethodPatch, caminhoCom("/v1/coupons", id), p.corpo(), &cp, nil); err != nil {
		return nil, err
	}
	return &cp, nil
}
