package spolly_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	spolly "github.com/spolly-ao/spolly-payments-go"
)

// apiDeEnsaio faz de API do Spolly Payments, para os exemplos correrem
// sozinhos. Responde como a API: com os mesmos corpos e os mesmos erros.
func apiDeEnsaio() *httptest.Server {
	vistas := map[string]bool{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		corpo, _ := io.ReadAll(r.Body)
		chave := r.Header.Get(spolly.IdempotencyKeyHeader)
		if vistas[chave] {
			w.Header().Set(spolly.IdempotentReplayedHeader, "true")
		}
		vistas[chave] = true
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/conta":
			io.WriteString(w, `{"merchant_id":"mer_01JA9SALUTO000000","nome_comercial":"Saluto","modo":"test"}`)
		case "POST /v1/checkout/sessions":
			if strings.Contains(string(corpo), `"currency":""`) {
				w.WriteHeader(422)
				io.WriteString(w, `{"erro":"dados_invalidos","mensagem":"Há campos por corrigir.","campos":{"currency":"Falta a moeda: AOA, USD ou EUR."}}`)
				return
			}
			if strings.Contains(string(corpo), `"platform_fee"`) {
				if strings.Contains(string(corpo), `"amount":900000}`) {
					w.WriteHeader(422)
					io.WriteString(w, `{"erro":"comissao_acima_do_maximo","mensagem":"A comissão passa o máximo que o comerciante autorizou (1000 pontos-base, 150000 unidades menores nesta sessão)."}`)
					return
				}
				s := sessao("open", `null`, `null`)
				w.WriteHeader(201)
				io.WriteString(w, s[:len(s)-1]+`,"application":"app_7Kq2Lm4Rt8Vx1Zb6Nc3Kd0","platform_fee":{"platform":"mer_38IAs7qmknSwC1F9XYtR1b","amount":150000}}`)
				return
			}
			if strings.Contains(string(corpo), `"first_cycle_amount":250000`) {
				s := strings.Replace(sessao("open", `null`, `null`), `"amount":1500000`, `"amount":500000`, 1)
				w.WriteHeader(201)
				io.WriteString(w, s[:len(s)-1]+`,"kind":"subscription","subscription":{"id":null,"interval":"month","cycles":null,"cycle":null,"first_cycle_amount":250000}}`)
				return
			}
			w.WriteHeader(201)
			io.WriteString(w, sessao("open", `null`, `null`))
		case "GET /v1/checkout/sessions/cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe":
			io.WriteString(w, sessao("complete", `"2026-11-29T10:17:12Z"`, `{"id":"pay_9Hq2Lm4Rt8Vx1Zb6Nc3Kd0","status":"succeeded","method":"mcx"}`))
		case "POST /v1/refunds":
			if strings.Contains(string(corpo), `"amount":9000000`) {
				w.WriteHeader(422)
				io.WriteString(w, `{"erro":"estorno_acima_do_cobrado","mensagem":"O estorno passa o que resta do pagamento: restam 1000000 AOA (em unidades menores). Nunca se estorna mais do que se cobrou."}`)
				return
			}
			w.WriteHeader(201)
			io.WriteString(w, estorno("pending"))
		case "GET /v1/refunds/re_7Tq3Mn5Wx9Yz2Ab4Cd6Ef8":
			io.WriteString(w, estorno("succeeded"))
		case "GET /v1/subscriptions/sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0":
			io.WriteString(w, subscricao(false))
		case "POST /v1/subscriptions/sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0/cancel":
			io.WriteString(w, subscricao(true))
		case "POST /v1/subscriptions/sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0/portal":
			w.WriteHeader(201)
			io.WriteString(w, `{"object":"subscription_portal","subscription":"sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0","url":"https://checkout.payments.spolly.ao/subscricao/l_9fK2","expires_at":"2026-12-02T09:00:00Z"}`)
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"erro":"sessao_nao_existe","mensagem":"Não há nenhuma sessão com este identificador para esta chave."}`)
		}
	}))
}

func sessao(estado, completa, pagamento string) string {
	return `{"id":"cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe","object":"checkout_session","mode":"test","status":"` + estado + `",
"url":"https://checkout.payments.spolly.ao/cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe","amount":{"amount":1500000,"currency":"AOA"},
"description":"Vestido de capulana","line_items":[],"payment_methods":["mcx","reference","card"],"success_url":"https://kiala.ao/obrigado?pedido=1042",
"cancel_url":null,"metadata":{"order_id":"1042"},"customer":{"email":"comprador@exemplo.ao","name":"","phone":""},
"expires_at":"2026-11-30T10:15:00Z","created_at":"2026-11-29T10:15:00Z","completed_at":` + completa + `,"payment":` + pagamento + `}`
}

func subscricao(cancelada bool) string {
	noFim := "false"
	if cancelada {
		noFim = "true"
	}
	return `{"id":"sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0","object":"subscription","status":"active","amount":{"amount":500000,"currency":"AOA"},
"interval":"month","cycles":12,"cycle":2,"current_period_start":"2026-11-29T10:15:00Z","current_period_end":"2026-12-29T10:15:00Z",
"method":"mcx","cancel_at_period_end":` + noFim + `,"checkout_session_id":"cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe",
"customer":{"email":"c@exemplo.ao","name":"","phone":""},"metadata":{}}`
}

func estorno(estado string) string {
	return `{"id":"re_7Tq3Mn5Wx9Yz2Ab4Cd6Ef8","object":"refund","payment_id":"pay_9Hq2Lm4Rt8Vx1Zb6Nc3Kd0",
"amount":{"amount":500000,"currency":"AOA"},"status":"` + estado + `","reason":"Devolução parcial a pedido do cliente.","created_at":"2026-12-02T09:00:00Z"}`
}

// Criar uma sessão e mandar o comprador para o URL do checkout alojado. O
// pagamento confirma-se depois, pelo webhook payment.succeeded.
func ExampleClient_CreateCheckoutSession() {
	api := apiDeEnsaio()
	defer api.Close()

	// Em produção: spolly.NewClient(os.Getenv("SPOLLY_SECRET_KEY")).
	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	sessao, err := cliente.CreateCheckoutSession(context.Background(), &spolly.CheckoutSessionParams{
		Amount:      1500000, // 15.000,00 Kz
		Currency:    spolly.AOA,
		Description: "Vestido de capulana",
		SuccessURL:  "https://kiala.ao/obrigado?pedido=1042",
		Metadata:    map[string]string{"order_id": "1042"},
		Customer:    &spolly.Customer{Email: "comprador@exemplo.ao"},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(sessao.Status, sessao.Amount.Amount, sessao.Amount.Currency)
	fmt.Println(sessao.URL)
	// Output:
	// open 1500000 AOA
	// https://checkout.payments.spolly.ao/cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe
}

// No regresso do comprador, o servidor lê a sessão para mostrar o estado
// certo. O regresso, sozinho, não prova pagamento.
func ExampleClient_GetCheckoutSession() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	sessao, err := cliente.GetCheckoutSession(context.Background(), "cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe")
	if err != nil {
		fmt.Println(err)
		return
	}
	if sessao.Paid() {
		fmt.Println("pago por", sessao.Payment.Method, "no pagamento", sessao.Payment.ID)
	}
	// Output:
	// pago por mcx no pagamento pay_9Hq2Lm4Rt8Vx1Zb6Nc3Kd0
}

// Estornar parte de um pagamento. A Idempotency-Key própria (o pedido da loja)
// faz com que repetir a chamada depois de um reinício não estorne duas vezes.
func ExampleClient_CreateRefund() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	estorno, err := cliente.CreateRefund(context.Background(), &spolly.RefundParams{
		PaymentID: "pay_9Hq2Lm4Rt8Vx1Zb6Nc3Kd0",
		Amount:    500000,
		Reason:    "Devolução parcial a pedido do cliente.",
	}, spolly.WithIdempotencyKey("pedido-1042-estorno-1"))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(estorno.ID, estorno.Status, estorno.Amount.Amount)
	// Output:
	// re_7Tq3Mn5Wx9Yz2Ab4Cd6Ef8 pending 500000
}

// O desfecho de um estorno que ficou pendente.
func ExampleClient_GetRefund() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	estorno, err := cliente.GetRefund(context.Background(), "re_7Tq3Mn5Wx9Yz2Ab4Cd6Ef8")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(estorno.Status == spolly.RefundSucceeded)
	// Output:
	// true
}

// A primeira chamada depois de configurar uma chave: de quem é, e em que modo.
func ExampleClient_GetAccount() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	conta, err := cliente.GetAccount(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(conta.TradeName, conta.Mode)
	// Output:
	// Saluto test
}

// As recusas da API são *spolly.Error, com o código estável para o programa
// decidir e a mensagem em português para mostrar.
func ExampleError() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	_, err := cliente.CreateRefund(context.Background(), &spolly.RefundParams{PaymentID: "pay_9Hq2Lm4Rt8Vx1Zb6Nc3Kd0", Amount: 9000000})

	var e *spolly.Error
	if errors.As(err, &e) {
		fmt.Println(e.StatusCode, e.Code)
	}
	fmt.Println(errors.Is(err, &spolly.Error{Code: spolly.CodeRefundExceedsCharged}))

	_, err = cliente.CreateCheckoutSession(context.Background(), &spolly.CheckoutSessionParams{Amount: 100})
	if errors.As(err, &e) && e.Code == spolly.CodeInvalidData {
		fmt.Println("currency:", e.Fields["currency"])
	}
	// Output:
	// 422 estorno_acima_do_cobrado
	// true
	// currency: Falta a moeda: AOA, USD ou EUR.
}

// A mesma chave dá a mesma resposta: a API não cria a sessão outra vez.
func ExampleWithIdempotencyKey() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	p := &spolly.CheckoutSessionParams{Amount: 1500000, Currency: spolly.AOA, Description: "Vestido de capulana"}
	a, _ := cliente.CreateCheckoutSession(context.Background(), p, spolly.WithIdempotencyKey("pedido-1042"))
	b, _ := cliente.CreateCheckoutSession(context.Background(), p, spolly.WithIdempotencyKey("pedido-1042"))
	fmt.Println(a.ID == b.ID)
	// Output:
	// true
}

// Uma subscrição mensal, doze ciclos. O preço de cada ciclo é o Amount; a
// subscrição nasce com o primeiro pagamento.
func ExampleClient_CreateCheckoutSession_subscription() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	doze := 12
	sessao, err := cliente.CreateCheckoutSession(context.Background(), &spolly.CheckoutSessionParams{
		Amount:       500000,
		Currency:     spolly.AOA,
		Description:  "Plano mensal",
		Kind:         spolly.KindSubscription,
		Subscription: &spolly.SubscriptionPlan{Interval: spolly.IntervalMonth, Cycles: &doze},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(sessao.URL)
	// Output:
	// https://checkout.payments.spolly.ao/cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe
}

func ExampleClient_GetSubscription() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	sub, err := cliente.GetSubscription(context.Background(), "sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(sub.Status, "ciclo", sub.Cycle, "de", *sub.Cycles, "pago até", sub.CurrentPeriodEnd.Format("2006-01-02"))
	// Output:
	// active ciclo 2 de 12 pago até 2026-12-29
}

// Cancelar no fim do ciclo pago: continua ativa até lá, e não renova.
func ExampleClient_CancelSubscription() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	sub, err := cliente.CancelSubscription(context.Background(), "sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(sub.Status, sub.CancelAtPeriodEnd)
	// Output:
	// active true
}

// O link da página do cliente, para lhe mandar: abre uma vez, e expira.
func ExampleClient_CreateSubscriptionPortalLink() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	link, err := cliente.CreateSubscriptionPortalLink(context.Background(), "sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(link.URL)
	// Output:
	// https://checkout.payments.spolly.ao/subscricao/l_9fK2
}

// Em desenvolvimento, contra a API local, com menos paciência e um nome no
// User-Agent.
func ExampleNewClient() {
	cliente := spolly.NewClient("sk_test_exemplo",
		spolly.WithBaseURL("http://localhost:8084"),
		spolly.WithMaxRetries(1),
		spolly.WithUserAgent("kiala-loja/1.4"),
	)
	fmt.Println(cliente != nil)
	// Output:
	// true
}

// Uma plataforma que vende em nome de um comerciante cria a sessão com a chave
// dele e fica com uma comissão no próprio pagamento. O comerciante tem de a
// ter autorizado no painel; acima do máximo dele, a API recusa.
func ExampleClient_CreateCheckoutSession_platformFee() {
	api := apiDeEnsaio()
	defer api.Close()

	hotel := spolly.NewClient("sk_test_chave_do_hotel", spolly.WithBaseURL(api.URL))
	sessao, err := hotel.CreateCheckoutSession(context.Background(), &spolly.CheckoutSessionParams{
		Amount:      1500000,
		Currency:    spolly.AOA,
		Description: "Duas noites, quarto duplo",
		PlatformFee: &spolly.PlatformFee{Platform: "mer_38IAs7qmknSwC1F9XYtR1b", Amount: 150000},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(sessao.PlatformFee.Amount, *sessao.Application)

	_, err = hotel.CreateCheckoutSession(context.Background(), &spolly.CheckoutSessionParams{
		Amount:      1500000,
		Currency:    spolly.AOA,
		Description: "Duas noites, quarto duplo",
		PlatformFee: &spolly.PlatformFee{Platform: "mer_38IAs7qmknSwC1F9XYtR1b", Amount: 900000},
	})
	fmt.Println(spolly.ErrorCode(err) == spolly.CodePlatformFeeAboveMaximum)
	// Output:
	// 150000 app_7Kq2Lm4Rt8Vx1Zb6Nc3Kd0
	// true
}

// O pagamento de um payment.succeeded, com a comissão da plataforma: o Net é
// o que fica ao comerciante.
func ExamplePayment() {
	objeto := []byte(`{"id":"pay_9Hq2Lm4Rt8Vx1Zb6Nc3Kd0","object":"payment","checkout_session_id":"cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe",
"status":"succeeded","amount":{"amount":1500000,"currency":"AOA"},"fee":{"amount":37500,"currency":"AOA"},
"net":{"amount":1312500,"currency":"AOA"},"method":"mcx","created_at":"2026-11-29T10:17:12Z","metadata":{},
"platform_fee":{"platform":"mer_38IAs7qmknSwC1F9XYtR1b","amount":150000}}`)
	var p spolly.Payment
	if err := json.Unmarshal(objeto, &p); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(p.Net.Amount, p.PlatformFee.Platform, p.PlatformFee.Amount, p.PlatformFeeSkipped == "")
	// Output:
	// 1312500 mer_38IAs7qmknSwC1F9XYtR1b 150000 true
}

// Um cupão que vale só no primeiro mês: o primeiro ciclo cobra 2.500 Kz, e os
// seguintes o preço cheio, 5.000 Kz. No cartão, o Stripe renova sozinho pelo
// preço cheio.
func ExampleClient_CreateCheckoutSession_firstCycle() {
	api := apiDeEnsaio()
	defer api.Close()

	cliente := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	sessao, err := cliente.CreateCheckoutSession(context.Background(), &spolly.CheckoutSessionParams{
		Amount:       500000,
		Currency:     spolly.AOA,
		Description:  "Plano mensal",
		Kind:         spolly.KindSubscription,
		Subscription: &spolly.SubscriptionPlan{Interval: spolly.IntervalMonth, FirstCycleAmount: 250000},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(sessao.Amount.Amount, *sessao.Subscription.FirstCycleAmount)
	// Output:
	// 500000 250000
}
