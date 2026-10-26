package spolly

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const cupaoJSON = `{"id":"cpn_test_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0Ab","object":"coupon","mode":"test","code":"BEMVINDO10","name":"Boas-vindas",
"percent_off":10,"amount_off":null,"duration":"repeating","duration_cycles":3,"max_redemptions":100,"times_redeemed":4,
"expires_at":"2026-12-31T23:59:59Z","active":true,"valid":true,"metadata":{"campanha":"natal"},
"created_at":"2026-11-29T08:55:00Z","updated_at":"2026-11-29T08:55:00Z"}`

const cupaoFixoJSON = `{"id":"cpn_test_9Kq2Lm4Rt8Vx1Zb6Nc3Kd0Ab","object":"coupon","mode":"test","code":"MENOS500","name":"",
"percent_off":null,"amount_off":{"amount":50000,"currency":"AOA"},"duration":"once","duration_cycles":null,"max_redemptions":null,
"times_redeemed":0,"expires_at":null,"active":false,"valid":false,"metadata":{},
"created_at":"2026-11-29T08:55:00Z","updated_at":"2026-11-30T08:55:00Z"}`

func TestOsCupoes(t *testing.T) {
	var consultas []string
	c, s, _ := novoCliente(t, func(_ int, r *http.Request) (int, http.Header, string) {
		consultas = append(consultas, r.URL.RawQuery)
		switch {
		case r.Method == "POST":
			return 201, nil, cupaoJSON
		case r.Method == "PATCH":
			return 200, nil, cupaoFixoJSON
		case r.URL.Path == "/v1/coupons":
			return 200, nil, `{"object":"list","data":[` + cupaoJSON + `,` + cupaoFixoJSON + `],"has_more":true}`
		}
		return 200, nil, cupaoJSON
	})
	ctx := context.Background()
	fim := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	cp, err := c.CreateCoupon(ctx, &CouponParams{Code: "bemvindo10", Name: "Boas-vindas", PercentOff: 10,
		Duration: CouponRepeating, DurationCycles: 3, MaxRedemptions: 100, ExpiresAt: &fim,
		Metadata: map[string]string{"campanha": "natal"}}, WithIdempotencyKey("cupao-1"))
	if err != nil || cp.Code != "BEMVINDO10" || *cp.PercentOff != 10 || cp.AmountOff != nil || cp.Duration != CouponRepeating ||
		*cp.DurationCycles != 3 || *cp.MaxRedemptions != 100 || cp.TimesRedeemed != 4 || !cp.Valid || cp.ExpiresAt.Year() != 2026 {
		t.Fatalf("%+v %v", cp, err)
	}
	if _, err := c.CreateCoupon(ctx, &CouponParams{Code: "MENOS500", AmountOff: 50000, Currency: AOA, Duration: CouponOnce}); err != nil {
		t.Fatal(err)
	}
	l, err := c.ListCoupons(ctx, nil)
	if err != nil || len(l.Data) != 2 || !l.HasMore || l.Data[1].AmountOff.Amount != 50000 || l.Data[1].PercentOff != nil {
		t.Fatalf("%+v %v", l, err)
	}
	if _, err := c.ListCoupons(ctx, &ListCouponsParams{Limit: 5, StartingAfter: "cpn_test_1", Code: "bemvindo10"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListCoupons(ctx, &ListCouponsParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetCoupon(ctx, "cpn_test_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0Ab"); err != nil {
		t.Fatal(err)
	}
	nao, nome, vinte := false, "Natal", 20
	u, err := c.UpdateCoupon(ctx, "cpn_test_9Kq2Lm4Rt8Vx1Zb6Nc3Kd0Ab", &CouponUpdateParams{Active: &nao})
	if err != nil || u.Active || u.Valid {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err := c.UpdateCoupon(ctx, "cpn_1", &CouponUpdateParams{Name: &nome, MaxRedemptions: &vinte, ExpiresAt: &fim,
		Metadata: map[string]string{"a": "b"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateCoupon(ctx, "cpn_1", &CouponUpdateParams{RemoveMaxRedemptions: true, MaxRedemptions: &vinte, RemoveExpiresAt: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateCoupon(ctx, "cpn_1", nil); err != nil {
		t.Fatal(err)
	}
	p := s.pedidos
	if p[0].Metodo != "POST" || p[0].Caminho != "/v1/coupons" || p[0].Chave != "cupao-1" ||
		p[0].Corpo != `{"code":"bemvindo10","name":"Boas-vindas","percent_off":10,"duration":"repeating","duration_cycles":3,"max_redemptions":100,"expires_at":"2026-12-31T23:59:59Z","metadata":{"campanha":"natal"}}` {
		t.Fatalf("%+v", p[0])
	}
	if p[1].Corpo != `{"code":"MENOS500","amount_off":50000,"currency":"AOA","duration":"once"}` || !uuid4.MatchString(p[1].Chave) {
		t.Fatal(p[1].Corpo)
	}
	if consultas[2] != "" || consultas[3] != "code=bemvindo10&limit=5&starting_after=cpn_test_1" || consultas[4] != "" {
		t.Fatal(consultas)
	}
	if p[5].Metodo != "GET" || p[5].Caminho != "/v1/coupons/cpn_test_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0Ab" {
		t.Fatalf("%+v", p[5])
	}
	if p[6].Metodo != "PATCH" || p[6].Corpo != `{"active":false}` || p[6].Chave != "" ||
		p[7].Corpo != `{"expires_at":"2026-12-31T23:59:59Z","max_redemptions":20,"metadata":{"a":"b"},"name":"Natal"}` ||
		p[8].Corpo != `{"expires_at":null,"max_redemptions":null}` || p[9].Corpo != `{}` {
		t.Fatalf("%+v", p[6:])
	}
	// As recusas e as respostas que não se leem.
	c, _, _ = novoCliente(t, sempre(409, `{"erro":"codigo_repetido","mensagem":"Já há."}`))
	if _, err := c.CreateCoupon(ctx, &CouponParams{Code: "X", Duration: CouponForever}); ErrorCode(err) != CodeCouponCodeRepeated {
		t.Fatal(err)
	}
	c, _, _ = novoCliente(t, sempre(404, `{"erro":"cupao_nao_existe","mensagem":"Não há."}`))
	if _, err := c.GetCoupon(ctx, "cpn_1"); ErrorCode(err) != CodeCouponNotFound {
		t.Fatal(err)
	}
	if _, err := c.UpdateCoupon(ctx, "cpn_1", nil); ErrorCode(err) != CodeCouponNotFound {
		t.Fatal(err)
	}
	if _, err := c.ListCoupons(ctx, nil); ErrorCode(err) != CodeCouponNotFound {
		t.Fatal(err)
	}
	for _, corpo := range []string{`{"object":"list","data":null}`, `{"object":"list","data":[{"id":"x"}]}`, `{"object":"x","data":[]}`} {
		c, _, _ = novoCliente(t, sempre(200, corpo))
		if l, err := c.ListCoupons(ctx, nil); l != nil || !errors.Is(err, ErrInvalidResponse) {
			t.Fatal(corpo, err)
		}
	}
	c, _, _ = novoCliente(t, sempre(200, `{"id":"cpn_1","object":"conta"}`))
	if cp, err := c.GetCoupon(ctx, "cpn_1"); cp != nil || !errors.Is(err, ErrInvalidResponse) {
		t.Fatal(err)
	}
}

func TestUmaSessaoComCupao(t *testing.T) {
	resposta := strings.Replace(sessaoJSON, `"payment":{`, `"allow_promotion_codes":false,"discount":{"coupon":{"id":"cpn_test_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0Ab","code":"BEMVINDO10",
"name":"Boas-vindas","percent_off":10,"amount_off":null,"duration":"once","duration_cycles":null},"amount":{"amount":150000,"currency":"AOA"}},"payment":{`, 1)
	c, s, _ := novoCliente(t, sempre(201, resposta))
	ctx := context.Background()
	se, err := c.CreateCheckoutSession(ctx, &CheckoutSessionParams{Amount: 1500000, Currency: AOA, Description: "x", Coupon: "bemvindo10"})
	if err != nil || se.Discount == nil || se.Discount.Amount.Amount != 150000 || se.Discount.Coupon.Code != "BEMVINDO10" ||
		*se.Discount.Coupon.PercentOff != 10 || se.AllowPromotionCodes {
		t.Fatalf("%+v %v", se, err)
	}
	if _, err := c.CreateCheckoutSession(ctx, &CheckoutSessionParams{Amount: 1500000, Currency: AOA, Description: "x", AllowPromotionCodes: true}); err != nil {
		t.Fatal(err)
	}
	if s.pedidos[0].Corpo != `{"amount":1500000,"currency":"AOA","description":"x","coupon":"bemvindo10"}` ||
		s.pedidos[1].Corpo != `{"amount":1500000,"currency":"AOA","description":"x","allow_promotion_codes":true}` {
		t.Fatal(s.pedidos[0].Corpo, s.pedidos[1].Corpo)
	}
	c, _, _ = novoCliente(t, sempre(422, `{"erro":"cupao_invalido","mensagem":"Este cupão expirou."}`))
	if _, err := c.CreateCheckoutSession(ctx, &CheckoutSessionParams{Amount: 1500000, Currency: AOA, Description: "x", Coupon: "VELHO"}); ErrorCode(err) != CodeCouponInvalid {
		t.Fatal(err)
	}
}

// O pagamento e a subscrição dos eventos levam o desconto.
func TestOPagamentoEASubscricaoLevamODesconto(t *testing.T) {
	var p Payment
	if err := json.Unmarshal([]byte(`{"id":"pay_1","object":"payment","amount":{"amount":1350000,"currency":"AOA"},
"discount":{"coupon":{"id":"cpn_1","code":"BEMVINDO10","name":"","percent_off":10,"amount_off":null,"duration":"once","duration_cycles":null},
"amount":{"amount":150000,"currency":"AOA"}}}`), &p); err != nil || p.Discount.Amount.Amount != 150000 {
		t.Fatal(p, err)
	}
	var sub Subscription
	if err := json.Unmarshal([]byte(`{"id":"sub_1","discount":{"coupon":{"id":"cpn_1","code":"TRES"},"amount":{"amount":50000,"currency":"AOA"},"cycles":3}}`), &sub); err != nil ||
		sub.Discount.Coupon.Code != "TRES" || *sub.Discount.Cycles != 3 {
		t.Fatal(sub, err)
	}
}
