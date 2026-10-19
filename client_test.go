package spolly

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const sessaoJSON = `{"id":"cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe","object":"checkout_session","mode":"test","status":"complete",
"url":"https://checkout.payments.spolly.ao/cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe","amount":{"amount":1500000,"currency":"AOA"},
"description":"Vestido de capulana","line_items":[],"payment_methods":["mcx","reference","card"],"success_url":"https://kiala.ao/obrigado",
"cancel_url":null,"metadata":{"order_id":"1042"},"customer":{"email":"","name":"","phone":""},"expires_at":"2026-11-30T10:15:00Z",
"created_at":"2026-11-29T10:15:00Z","completed_at":"2026-11-29T10:17:12Z","payment":{"id":"pay_9Hq2Lm4Rt8Vx1Zb6Nc3Kd0","status":"succeeded","method":"mcx"}}`

const estornoJSON = `{"id":"re_7Tq3Mn5Wx9Yz2Ab4Cd6Ef8","object":"refund","payment_id":"pay_9Hq2Lm4Rt8Vx1Zb6Nc3Kd0",
"amount":{"amount":500000,"currency":"AOA"},"status":"pending","reason":"Devolução parcial","created_at":"2026-12-02T09:00:00Z"}`

// gravado é um pedido que o servidor de ensaio recebeu.
type gravado struct {
	Metodo, Caminho, Chave, Auth, Agente, Tipo string
	Corpo                                      string
}

// servidor responde pela ordem da lista de respostas, e grava os pedidos.
type servidor struct {
	mu       sync.Mutex
	pedidos  []gravado
	resposta func(n int, r *http.Request) (int, http.Header, string)
}

func (s *servidor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.pedidos = append(s.pedidos, gravado{r.Method, r.URL.EscapedPath(), r.Header.Get(IdempotencyKeyHeader),
		r.Header.Get("Authorization"), r.Header.Get("User-Agent"), r.Header.Get("Content-Type"), string(b)})
	n := len(s.pedidos)
	s.mu.Unlock()
	estado, h, corpo := s.resposta(n, r)
	for k, v := range h {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(estado)
	_, _ = io.WriteString(w, corpo)
}

func novoCliente(t *testing.T, f func(n int, r *http.Request) (int, http.Header, string), opts ...Option) (*Client, *servidor, *[]time.Duration) {
	t.Helper()
	s := &servidor{resposta: f}
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	c := NewClient("sk_test_abc", append([]Option{WithBaseURL(ts.URL + "/")}, opts...)...)
	esperas := &[]time.Duration{}
	c.dormir = func(_ context.Context, d time.Duration) error { *esperas = append(*esperas, d); return nil }
	c.aleatorio = func(n int64) int64 { return n - 1 }
	return c, s, esperas
}

func sempre(estado int, corpo string) func(int, *http.Request) (int, http.Header, string) {
	return func(int, *http.Request) (int, http.Header, string) { return estado, nil, corpo }
}

var uuid4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestCriarSessaoMandaOPedidoCertoComChaveGerada(t *testing.T) {
	c, s, _ := novoCliente(t, sempre(201, sessaoJSON), WithUserAgent("loja/1.4"), WithUserAgent(""))
	sessao, err := c.CreateCheckoutSession(context.Background(), &CheckoutSessionParams{
		Amount: 1500000, Currency: AOA, Description: "Vestido de capulana",
		PaymentMethods: []PaymentMethod{MethodMCX, MethodCard}, SuccessURL: "https://kiala.ao/obrigado",
		Metadata: map[string]string{"order_id": "1042"}, Customer: &Customer{Email: "c@exemplo.ao"}, ExpiresIn: 900,
		LineItems: []LineItem{{Description: "Vestido", UnitAmount: 1500000, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sessao.Paid() || sessao.Amount != (Money{1500000, AOA}) || sessao.Payment.Method != MethodMCX || sessao.CancelURL != nil ||
		*sessao.SuccessURL != "https://kiala.ao/obrigado" || sessao.CompletedAt == nil || sessao.Mode != ModeTest {
		t.Fatalf("%+v", sessao)
	}
	p := s.pedidos[0]
	if p.Metodo != "POST" || p.Caminho != "/v1/checkout/sessions" || p.Auth != "Bearer sk_test_abc" || p.Tipo != "application/json" ||
		p.Agente != "loja/1.4 spolly-payments-go/"+Version || !uuid4.MatchString(p.Chave) {
		t.Fatalf("%+v", p)
	}
	want := `{"amount":1500000,"currency":"AOA","description":"Vestido de capulana","line_items":[{"description":"Vestido","unit_amount":1500000,"quantity":1}],"payment_methods":["mcx","card"],"success_url":"https://kiala.ao/obrigado","metadata":{"order_id":"1042"},"customer":{"email":"c@exemplo.ao"},"expires_in":900}`
	if p.Corpo != want {
		t.Fatalf("corpo:\n%s\nesperado:\n%s", p.Corpo, want)
	}
}

func TestOsCamposVaziosNaoViajam(t *testing.T) {
	c, s, _ := novoCliente(t, sempre(201, sessaoJSON))
	if _, err := c.CreateCheckoutSession(context.Background(), &CheckoutSessionParams{Amount: 100, Currency: USD}); err != nil {
		t.Fatal(err)
	}
	if s.pedidos[0].Corpo != `{"amount":100,"currency":"USD"}` {
		t.Fatal(s.pedidos[0].Corpo)
	}
}

func TestAsRetentativasUsamAMesmaChaveERespeitamORetryAfter(t *testing.T) {
	c, s, esperas := novoCliente(t, func(n int, _ *http.Request) (int, http.Header, string) {
		switch n {
		case 1:
			return 503, http.Header{"Retry-After": {"3"}}, `{"erro":"indisponivel","mensagem":"Tenta de novo."}`
		case 2:
			return 500, nil, `{"erro":"erro_interno","mensagem":"x"}`
		}
		return 201, http.Header{IdempotentReplayedHeader: {"true"}}, estornoJSON
	})
	r, err := c.CreateRefund(context.Background(), &RefundParams{PaymentID: "pay_1", Amount: 500000, Reason: "Devolução parcial"},
		WithIdempotencyKey("pedido-1042-estorno-1"))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "re_7Tq3Mn5Wx9Yz2Ab4Cd6Ef8" || r.Status != RefundPending || r.Amount.Amount != 500000 {
		t.Fatalf("%+v", r)
	}
	if len(s.pedidos) != 3 {
		t.Fatal(len(s.pedidos))
	}
	for _, p := range s.pedidos {
		if p.Chave != "pedido-1042-estorno-1" || p.Corpo != `{"payment_id":"pay_1","amount":500000,"reason":"Devolução parcial"}` {
			t.Fatalf("%+v", p)
		}
	}
	// O Retry-After de 3 segundos, e depois 1 segundo exponencial (metade fixa e a outra metade pelo aleatório).
	if len(*esperas) != 2 || (*esperas)[0] != 3*time.Second || (*esperas)[1] != time.Second {
		t.Fatal(*esperas)
	}
}

func TestAChaveGeradaEAMesmaEmTodasAsTentativas(t *testing.T) {
	c, s, _ := novoCliente(t, func(n int, _ *http.Request) (int, http.Header, string) {
		if n < 3 {
			return 502, nil, `<html>proxy</html>`
		}
		return 201, nil, sessaoJSON
	})
	if _, err := c.CreateCheckoutSession(context.Background(), &CheckoutSessionParams{Amount: 100, Currency: AOA}); err != nil {
		t.Fatal(err)
	}
	if len(s.pedidos) != 3 || s.pedidos[0].Chave != s.pedidos[1].Chave || s.pedidos[1].Chave != s.pedidos[2].Chave {
		t.Fatal(s.pedidos)
	}
	// Chamadas diferentes, chaves diferentes.
	if _, err := c.CreateCheckoutSession(context.Background(), &CheckoutSessionParams{Amount: 100, Currency: AOA}); err != nil {
		t.Fatal(err)
	}
	if s.pedidos[3].Chave == s.pedidos[0].Chave {
		t.Fatal("a mesma chave em duas chamadas")
	}
}

func TestEsgotarAsRetentativasDevolveOUltimoErro(t *testing.T) {
	c, s, esperas := novoCliente(t, sempre(503, `{"erro":"indisponivel","mensagem":"Não foi possível validar a chave. Tenta de novo."}`))
	_, err := c.GetCheckoutSession(context.Background(), "cs_test_1")
	var e *Error
	if !errors.As(err, &e) || e.StatusCode != 503 || e.Code != CodeUnavailable || e.IdempotencyKey != "" {
		t.Fatal(err)
	}
	if len(s.pedidos) != 1+DefaultMaxRetries || len(*esperas) != DefaultMaxRetries {
		t.Fatal(len(s.pedidos), *esperas)
	}
	// Exponencial: 500 ms e 1 s, sem passar o máximo.
	if (*esperas)[0] != 500*time.Millisecond || (*esperas)[1] != time.Second {
		t.Fatal(*esperas)
	}
	if s.pedidos[0].Metodo != "GET" || s.pedidos[0].Chave != "" || s.pedidos[0].Tipo != "" || s.pedidos[0].Corpo != "" {
		t.Fatalf("%+v", s.pedidos[0])
	}
}

func TestOs4xxNaoSeRepetem(t *testing.T) {
	c, s, _ := novoCliente(t, sempre(422, `{"erro":"dados_invalidos","mensagem":"Há campos por corrigir.","campos":{"currency":"Falta a moeda.","amount":"Falta o valor."}}`))
	_, err := c.CreateCheckoutSession(context.Background(), &CheckoutSessionParams{})
	if len(s.pedidos) != 1 {
		t.Fatal(len(s.pedidos))
	}
	var e *Error
	if !errors.As(err, &e) || e.Fields["currency"] != "Falta a moeda." || !uuid4.MatchString(e.IdempotencyKey) {
		t.Fatal(err)
	}
	if err.Error() != "spolly: 422 dados_invalidos: Há campos por corrigir.; amount: Falta o valor.; currency: Falta a moeda." {
		t.Fatal(err.Error())
	}
	if !errors.Is(err, &Error{Code: CodeInvalidData}) || errors.Is(err, &Error{Code: CodeInvalidKey}) ||
		!errors.Is(err, &Error{StatusCode: 422}) || errors.Is(err, &Error{}) || errors.Is(err, io.EOF) {
		t.Fatal("errors.Is")
	}
	if ErrorCode(err) != CodeInvalidData || ErrorCode(io.EOF) != "" {
		t.Fatal("ErrorCode")
	}
}

func TestUmErroIlegivelNaoViraOutraCoisa(t *testing.T) {
	longo := strings.Repeat("x", 300)
	c, _, _ := novoCliente(t, sempre(404, longo), WithMaxRetries(-1))
	_, err := c.GetRefund(context.Background(), "re_1")
	var e *Error
	if !errors.As(err, &e) || e.StatusCode != 404 || e.Code != CodeUnreadableError || !strings.Contains(e.Message, strings.Repeat("x", 200)+"...") {
		t.Fatal(err)
	}
	c, _, _ = novoCliente(t, sempre(403, `{"mensagem":"sem código"}`))
	if _, err := c.GetAccount(context.Background()); ErrorCode(err) != CodeUnreadableError {
		t.Fatal(err)
	}
}

func TestUmaRespostaIlegivelNuncaEUmObjetoVazio(t *testing.T) {
	for _, corpo := range []string{`{}`, `não é json`, `{"id":"pay_1","object":"payment"}`, `[]`} {
		c, _, _ := novoCliente(t, sempre(200, corpo))
		s, err := c.GetCheckoutSession(context.Background(), "cs_test_1")
		if s != nil || !errors.Is(err, ErrInvalidResponse) {
			t.Fatal(corpo, s, err)
		}
		r, err := c.GetRefund(context.Background(), "re_1")
		if r != nil || !errors.Is(err, ErrInvalidResponse) {
			t.Fatal(corpo, r, err)
		}
		a, err := c.GetAccount(context.Background())
		if a != nil || !errors.Is(err, ErrInvalidResponse) {
			t.Fatal(corpo, a, err)
		}
	}
	c, _, _ := novoCliente(t, sempre(200, `{"merchant_id":"mer_1","nome_comercial":"Kiala","modo":"producao"}`))
	if _, err := c.GetAccount(context.Background()); !errors.Is(err, ErrInvalidResponse) {
		t.Fatal(err)
	}
}

func TestOperacoes(t *testing.T) {
	c, s, _ := novoCliente(t, func(_ int, r *http.Request) (int, http.Header, string) {
		switch r.URL.Path {
		case "/v1/conta":
			return 200, nil, `{"merchant_id":"mer_01JA9SALUTO000000","nome_comercial":"Saluto","modo":"test"}`
		case "/v1/refunds/re_7Tq3Mn5Wx9Yz2Ab4Cd6Ef8":
			return 200, nil, estornoJSON
		}
		return 200, nil, sessaoJSON
	})
	ctx := context.Background()
	a, err := c.GetAccount(ctx)
	if err != nil || a.MerchantID != "mer_01JA9SALUTO000000" || a.TradeName != "Saluto" || a.Mode != ModeTest {
		t.Fatal(a, err)
	}
	r, err := c.GetRefund(ctx, "re_7Tq3Mn5Wx9Yz2Ab4Cd6Ef8")
	if err != nil || r.PaymentID != "pay_9Hq2Lm4Rt8Vx1Zb6Nc3Kd0" {
		t.Fatal(r, err)
	}
	if _, err := c.GetCheckoutSession(ctx, "cs_test_a/b?c"); err != nil {
		t.Fatal(err)
	}
	if got := s.pedidos[2].Caminho; got != "/v1/checkout/sessions/cs_test_a%2Fb%3Fc" {
		t.Fatal(got)
	}
	// Os erros de cada operação.
	c, _, _ = novoCliente(t, sempre(401, `{"erro":"chave_invalida","mensagem":"A chave de API não é válida."}`))
	if _, err := c.CreateRefund(ctx, &RefundParams{PaymentID: "pay_1"}); ErrorCode(err) != CodeInvalidKey {
		t.Fatal(err)
	}
	if _, err := c.CreateCheckoutSession(ctx, &CheckoutSessionParams{}); ErrorCode(err) != CodeInvalidKey {
		t.Fatal(err)
	}
}

func TestSemChaveNaoChamaNada(t *testing.T) {
	c := NewClient("")
	if _, err := c.GetAccount(context.Background()); !errors.Is(err, ErrNoKey) {
		t.Fatal(err)
	}
}

func TestUmEnderecoQueNaoServe(t *testing.T) {
	c := NewClient("sk_test_1", WithBaseURL("http://[::1"))
	if _, err := c.GetAccount(context.Background()); err == nil || !strings.Contains(err.Error(), "endereço da API não serve") {
		t.Fatal(err)
	}
}

type transporte func(*http.Request) (*http.Response, error)

func (f transporte) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type corpoQueFalha struct{}

func (corpoQueFalha) Read([]byte) (int, error) { return 0, errors.New("a ligação caiu a meio") }
func (corpoQueFalha) Close() error             { return nil }

func TestAFalhaDeRedeRepeteComOMesmoCorpo(t *testing.T) {
	var n atomic.Int32
	var corpos []string
	var chaves []string
	h := &http.Client{Transport: transporte(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		corpos = append(corpos, string(b))
		chaves = append(chaves, r.Header.Get(IdempotencyKeyHeader))
		switch n.Add(1) {
		case 1:
			return nil, errors.New("ligação recusada")
		case 2:
			return &http.Response{StatusCode: 201, Body: corpoQueFalha{}, Header: http.Header{}}, nil
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(sessaoJSON)), Header: http.Header{}}, nil
	})}
	c := NewClient("sk_test_1", WithHTTPClient(h), WithHTTPClient(nil))
	c.dormir = func(context.Context, time.Duration) error { return nil }
	if _, err := c.CreateCheckoutSession(context.Background(), &CheckoutSessionParams{Amount: 100, Currency: AOA}); err != nil {
		t.Fatal(err)
	}
	if len(corpos) != 3 || corpos[0] != corpos[2] || corpos[0] != `{"amount":100,"currency":"AOA"}` || chaves[0] != chaves[2] || chaves[0] == "" {
		t.Fatal(corpos, chaves)
	}
	// Sem retentativas, a falha de rede chega a quem chamou, embrulhada.
	n.Store(0)
	c = NewClient("sk_test_1", WithHTTPClient(h), WithMaxRetries(0))
	_, err := c.CreateCheckoutSession(context.Background(), &CheckoutSessionParams{Amount: 100, Currency: AOA})
	if err == nil || !strings.Contains(err.Error(), "ligação recusada") || ErrorCode(err) != "" {
		t.Fatal(err)
	}
}

func TestOTempoLimiteDeCadaTentativa(t *testing.T) {
	bloqueia := make(chan struct{})
	t.Cleanup(func() { close(bloqueia) })
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-bloqueia:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(ts.Close)
	c := NewClient("sk_test_1", WithBaseURL(ts.URL), WithTimeout(50*time.Millisecond), WithTimeout(0), WithMaxRetries(1))
	c.dormir = func(context.Context, time.Duration) error { return nil }
	inicio := time.Now()
	_, err := c.GetAccount(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(inicio) > 5*time.Second {
		t.Fatal(err, time.Since(inicio))
	}
}

func TestOCtxDeQuemChamaMandaSobreAsRetentativas(t *testing.T) {
	c, s, _ := novoCliente(t, sempre(503, `{"erro":"indisponivel","mensagem":"x"}`))
	ctx, cancelar := context.WithCancel(context.Background())
	c.dormir = func(ctx context.Context, _ time.Duration) error { cancelar(); return ctx.Err() }
	if _, err := c.GetAccount(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(s.pedidos) != 1 {
		t.Fatal(len(s.pedidos))
	}
	// Já cancelado antes de responder: não volta a tentar.
	ctx2, cancelar2 := context.WithCancel(context.Background())
	c.httpClient = &http.Client{Transport: transporte(func(r *http.Request) (*http.Response, error) {
		cancelar2()
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"erro":"indisponivel","mensagem":"x"}`)), Header: http.Header{}}, nil
	})}
	if _, err := c.GetAccount(ctx2); ErrorCode(err) != CodeUnavailable {
		t.Fatal(err)
	}
}

func TestEspera(t *testing.T) {
	c := NewClient("sk")
	c.aleatorio = func(n int64) int64 { return 0 }
	casos := []struct {
		tentativa int
		h         http.Header
		want      time.Duration
	}{
		{0, nil, 250 * time.Millisecond},
		{3, http.Header{}, 2 * time.Second},
		{10, http.Header{"Retry-After": {"lixo"}}, esperaMaxima / 2},
		{0, http.Header{"Retry-After": {"0"}}, 0},
		{0, http.Header{"Retry-After": {"3600"}}, retryAfterMaximo},
		{0, http.Header{"Retry-After": {"-1"}}, 250 * time.Millisecond},
	}
	for _, k := range casos {
		if got := c.espera(k.tentativa, k.h); got != k.want {
			t.Errorf("%d %v: %v, esperado %v", k.tentativa, k.h, got, k.want)
		}
	}
	// O aleatório de verdade fica entre metade e o todo.
	c = NewClient("sk")
	for range 100 {
		if d := c.espera(1, nil); d < 500*time.Millisecond || d > time.Second {
			t.Fatal(d)
		}
	}
}

func TestDormir(t *testing.T) {
	if err := dormir(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	if err := dormir(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestNewIdempotencyKey(t *testing.T) {
	vistos := map[string]bool{}
	for range 1000 {
		k := NewIdempotencyKey()
		if !uuid4.MatchString(k) || vistos[k] {
			t.Fatal(k)
		}
		vistos[k] = true
	}
}

func TestOsTiposBatemComOContrato(t *testing.T) {
	// Os exemplos do contratos/openapi.yaml lidos para os tipos e escritos de
	// volta dão os mesmos campos.
	for _, k := range []struct {
		corpo string
		dst   any
	}{{sessaoJSON, &CheckoutSession{}}, {estornoJSON, &Refund{}}} {
		if err := json.Unmarshal([]byte(k.corpo), k.dst); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(k.dst)
		var a, d map[string]any
		_ = json.Unmarshal([]byte(k.corpo), &a)
		_ = json.Unmarshal(b, &d)
		for campo := range a {
			if _, ok := d[campo]; !ok {
				t.Errorf("o campo %q não está no tipo", campo)
			}
		}
	}
	s := CheckoutSession{Status: SessionOpen}
	if s.Paid() {
		t.Fatal("aberta e paga")
	}
	s = CheckoutSession{Status: SessionComplete, Payment: &SessionPayment{Status: "failed"}}
	if s.Paid() {
		t.Fatal("pagamento falhado e paga")
	}
}

func TestUmaSessaoDeSubscricao(t *testing.T) {
	resposta := strings.Replace(sessaoJSON, `"payment":{`, `"kind":"subscription","subscription":{"id":null,"interval":"month","cycles":12,"cycle":null},"payment":{`, 1)
	c, s, _ := novoCliente(t, sempre(201, resposta))
	doze := 12
	sessao, err := c.CreateCheckoutSession(context.Background(), &CheckoutSessionParams{
		Amount: 500000, Currency: AOA, Description: "Plano mensal", Kind: KindSubscription,
		Subscription: &SubscriptionPlan{Interval: IntervalMonth, Cycles: &doze},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.pedidos[0].Corpo != `{"amount":500000,"currency":"AOA","description":"Plano mensal","kind":"subscription","subscription":{"interval":"month","cycles":12}}` {
		t.Fatal(s.pedidos[0].Corpo)
	}
	if sessao.Kind != KindSubscription || sessao.Subscription == nil || sessao.Subscription.ID != nil ||
		sessao.Subscription.Interval != IntervalMonth || *sessao.Subscription.Cycles != 12 || sessao.Subscription.Cycle != nil {
		t.Fatalf("%+v", sessao.Subscription)
	}
	// Sem fim: cycles não viaja.
	if _, err := c.CreateCheckoutSession(context.Background(), &CheckoutSessionParams{Amount: 500000, Currency: AOA, Description: "x",
		Kind: KindSubscription, Subscription: &SubscriptionPlan{Interval: IntervalYear}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(s.pedidos[1].Corpo, `"subscription":{"interval":"year"}}`) {
		t.Fatal(s.pedidos[1].Corpo)
	}
}

const subscricaoJSON = `{"id":"sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0","object":"subscription","status":"active","amount":{"amount":500000,"currency":"AOA"},
"interval":"month","cycles":null,"cycle":2,"current_period_start":"2026-11-29T10:15:00Z","current_period_end":"2026-12-29T10:15:00Z",
"method":"mcx","renewal":"notice","cancel_at_period_end":true,"cancelled_at":"2026-12-01T09:00:00Z","cancelled_by":"merchant",
"checkout_session_id":"cs_test_4f8Gk2LmQ9rT1vXz7bN3cH6sW0pY5aDe","latest_payment_id":"pay_1","customer":{"email":"c@exemplo.ao","name":"","phone":""},"metadata":{"plano":"mensal"}}`

func TestSubscricoes(t *testing.T) {
	c, s, _ := novoCliente(t, func(_ int, r *http.Request) (int, http.Header, string) {
		if strings.HasSuffix(r.URL.Path, "/portal") {
			return 201, nil, `{"object":"subscription_portal","subscription":"sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0","url":"https://checkout.payments.spolly.ao/subscricao/l_abc","expires_at":"2026-12-02T09:00:00Z"}`
		}
		return 200, nil, subscricaoJSON
	})
	ctx := context.Background()
	sub, err := c.GetSubscription(ctx, "sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0")
	if err != nil || sub.Status != SubscriptionActive || sub.Cycles != nil || sub.Cycle != 2 || !sub.CancelAtPeriodEnd ||
		*sub.CancelledBy != "merchant" || sub.Amount.Amount != 500000 || sub.Method != MethodMCX || sub.Renewal != RenewalNotice || sub.CurrentPeriodEnd.Day() != 29 {
		t.Fatalf("%+v %v", sub, err)
	}
	if _, err := c.CancelSubscription(ctx, "sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0", nil); err != nil {
		t.Fatal(err)
	}
	ja := false
	if _, err := c.CancelSubscription(ctx, "sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0", &CancelSubscriptionParams{AtPeriodEnd: &ja}, WithIdempotencyKey("cancelar-1")); err != nil {
		t.Fatal(err)
	}
	l, err := c.CreateSubscriptionPortalLink(ctx, "sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0")
	if err != nil || l.URL != "https://checkout.payments.spolly.ao/subscricao/l_abc" || l.Subscription != "sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0" || l.ExpiresAt.IsZero() {
		t.Fatal(l, err)
	}
	p := s.pedidos
	if p[0].Metodo != "GET" || p[0].Caminho != "/v1/subscriptions/sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0" ||
		p[1].Metodo != "POST" || p[1].Caminho != "/v1/subscriptions/sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0/cancel" || p[1].Corpo != `{}` || !uuid4.MatchString(p[1].Chave) ||
		p[2].Corpo != `{"at_period_end":false}` || p[2].Chave != "cancelar-1" ||
		p[3].Caminho != "/v1/subscriptions/sub_8Kq2Lm4Rt8Vx1Zb6Nc3Kd0/portal" || p[3].Corpo != "" || p[3].Tipo != "" || !uuid4.MatchString(p[3].Chave) {
		t.Fatalf("%+v", p)
	}
	// As recusas e as respostas que não se leem.
	c, _, _ = novoCliente(t, sempre(409, `{"erro":"subscricao_terminada","mensagem":"Esta subscrição já terminou."}`))
	if _, err := c.CancelSubscription(ctx, "sub_1", nil); ErrorCode(err) != CodeSubscriptionEnded {
		t.Fatal(err)
	}
	if _, err := c.CreateSubscriptionPortalLink(ctx, "sub_1"); ErrorCode(err) != CodeSubscriptionEnded {
		t.Fatal(err)
	}
	c, _, _ = novoCliente(t, sempre(404, `{"erro":"subscricao_nao_existe","mensagem":"Não há."}`))
	if _, err := c.GetSubscription(ctx, "sub_1"); ErrorCode(err) != CodeSubscriptionNotFound {
		t.Fatal(err)
	}
	c, _, _ = novoCliente(t, sempre(201, `{"object":"subscription_portal","url":""}`))
	if l, err := c.CreateSubscriptionPortalLink(ctx, "sub_1"); l != nil || !errors.Is(err, ErrInvalidResponse) {
		t.Fatal(err)
	}
}
