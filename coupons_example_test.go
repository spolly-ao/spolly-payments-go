package spolly_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	spolly "github.com/spolly-ao/spolly-payments-go"
)

// apiDosCupoes faz de API do Spolly Payments para os exemplos dos cupões, com
// os corpos do contrato.
func apiDosCupoes() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ativo := "true"
		if r.Method == http.MethodPatch {
			ativo = "false"
		}
		cupao := `{"id":"cpn_test_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0Ab","object":"coupon","mode":"test","code":"BEMVINDO10","name":"Boas-vindas",
"percent_off":10,"amount_off":null,"duration":"repeating","duration_cycles":3,"max_redemptions":100,"times_redeemed":4,
"expires_at":null,"active":` + ativo + `,"valid":` + ativo + `,"metadata":{},"created_at":"2026-11-29T08:55:00Z","updated_at":"2026-11-29T08:55:00Z"}`
		switch {
		case r.Method == http.MethodPost:
			w.WriteHeader(201)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/coupons":
			cupao = `{"object":"list","data":[` + cupao + `],"has_more":false}`
		}
		io.WriteString(w, cupao)
	}))
}

// Um cupão de 10% nos três primeiros ciclos de uma subscrição, com 100 usos.
func ExampleClient_CreateCoupon() {
	api := apiDosCupoes()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	cupao, err := cliente.CreateCoupon(context.Background(), &spolly.CouponParams{
		Code: "bemvindo10", Name: "Boas-vindas", PercentOff: 10,
		Duration: spolly.CouponRepeating, DurationCycles: 3, MaxRedemptions: 100,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(cupao.ID, cupao.Code, *cupao.PercentOff)
	// Output:
	// cpn_test_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0Ab BEMVINDO10 10
}

// Encontrar um cupão pelo código.
func ExampleClient_ListCoupons() {
	api := apiDosCupoes()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	pagina, err := cliente.ListCoupons(context.Background(), &spolly.ListCouponsParams{Code: "bemvindo10"})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, c := range pagina.Data {
		fmt.Println(c.Code, c.TimesRedeemed, c.Valid)
	}
	// Output:
	// BEMVINDO10 4 true
}

// Quantas vezes já se pagou com um cupão.
func ExampleClient_GetCoupon() {
	api := apiDosCupoes()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	cupao, err := cliente.GetCoupon(context.Background(), "cpn_test_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0Ab")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(cupao.TimesRedeemed, "de", *cupao.MaxRedemptions)
	// Output:
	// 4 de 100
}

// Desativar um cupão: deixa de se aplicar, e quem já o tem fica com ele.
func ExampleClient_UpdateCoupon() {
	api := apiDosCupoes()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	nao := false
	cupao, err := cliente.UpdateCoupon(context.Background(), "cpn_test_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0Ab", &spolly.CouponUpdateParams{Active: &nao})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(cupao.Active, cupao.Valid)
	// Output:
	// false false
}
