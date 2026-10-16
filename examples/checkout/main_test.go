package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	spolly "github.com/spolly-ao/spolly-payments-go"
	"github.com/spolly-ao/spolly-payments-go/webhook"
)

const segredo = "whsec_ensaio_do_exemplo"

// apiFalsa responde como a API, e marca a sessão como paga quando paga() é
// chamada.
func apiFalsa(t *testing.T) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	paga := &atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/conta":
			io.WriteString(w, `{"merchant_id":"mer_1","nome_comercial":"Kiala","modo":"test"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/checkout/sessions":
			if r.Header.Get(spolly.IdempotencyKeyHeader) == "" {
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(201)
			io.WriteString(w, sessao(false))
		case r.URL.Path == "/v1/checkout/sessions/cs_test_1":
			io.WriteString(w, sessao(paga.Load()))
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"erro":"sessao_nao_existe","mensagem":"Não há."}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, paga
}

func sessao(paga bool) string {
	estado, pagamento := "open", "null"
	if paga {
		estado, pagamento = "complete", `{"id":"pay_1","status":"succeeded","method":"mcx"}`
	}
	return `{"id":"cs_test_1","object":"checkout_session","mode":"test","status":"` + estado + `","url":"http://checkout/cs_test_1",
"amount":{"amount":250000,"currency":"AOA"},"description":"x","line_items":[],"payment_methods":["mcx"],"success_url":null,"cancel_url":null,
"metadata":{},"customer":{"email":"","name":"","phone":""},"expires_at":"2026-11-30T10:15:00Z","created_at":"2026-11-29T10:15:00Z",
"completed_at":null,"payment":` + pagamento + `}`
}

func enviar(t *testing.T, url, corpo, comSegredo string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(corpo))
	req.Header.Set(webhook.SignatureHeader, webhook.Sign(time.Now(), []byte(corpo), comSegredo))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func evento(id, sessao string) string {
	return fmt.Sprintf(`{"id":"%s","type":"payment.succeeded","created_at":"2026-10-16T10:00:00Z","mode":"test","merchant_id":"mer_1","api_version":"2026-10-16","data":{"object":{"id":"pay_1","checkout_session_id":"%s","amount":{"amount":250000,"currency":"AOA"}}}}`, id, sessao)
}

func TestOPercursoInteiro(t *testing.T) {
	api, paga := apiFalsa(t)
	var saida bytes.Buffer
	err := run(context.Background(), config{Chave: "sk_test_1", Segredos: []string{segredo}, API: api.URL, Endereco: "127.0.0.1:0", Valor: 250000, Espera: 10 * time.Second},
		&saida, func(s *spolly.CheckoutSession, url string) {
			// Um evento falsificado, um de outra sessão, um que não é pagamento,
			// um objeto ilegível, e por fim o verdadeiro.
			if enviar(t, url, evento("evt_falso", s.ID), "whsec_outro") != 400 {
				t.Error("aceitou um segredo errado")
			}
			if enviar(t, url, evento("evt_outra", "cs_test_outra"), segredo) != 200 {
				t.Error("outra sessão")
			}
			if enviar(t, url, `{"id":"evt_x","type":"refund.created","data":{"object":{}}}`, segredo) != 200 {
				t.Error("outro tipo")
			}
			if enviar(t, url, `{"id":"evt_y","type":"payment.succeeded","data":{"object":[]}}`, segredo) != 400 {
				t.Error("objeto ilegível")
			}
			paga.Store(true)
			if enviar(t, url, evento("evt_1", s.ID), segredo) != 200 {
				t.Error("o verdadeiro")
			}
		})
	if err != nil {
		t.Fatal(err, saida.String())
	}
	for _, want := range []string{"chave de Kiala (mer_1), modo test", "sessão cs_test_1 criada, 250000 AOA, open",
		"webhook recusado pelo SDK", "webhook evt_1: pagamento pay_1 de 250000 AOA, assinatura válida pelo SDK", "sessão cs_test_1 complete, paga por pay_1 (mcx)"} {
		if !strings.Contains(saida.String(), want) {
			t.Errorf("falta %q em:\n%s", want, saida.String())
		}
	}
}

func TestAsFalhas(t *testing.T) {
	api, _ := apiFalsa(t)
	base := config{Chave: "sk_test_1", Segredos: []string{segredo}, API: api.URL, Endereco: "127.0.0.1:0", Valor: 250000, Espera: 50 * time.Millisecond}
	ctx := context.Background()

	if err := run(ctx, config{}, io.Discard, nil); err == nil {
		t.Fatal("sem chave")
	}
	c := base
	c.API = "http://127.0.0.1:1"
	if err := run(ctx, c, io.Discard, nil); err == nil || !strings.Contains(err.Error(), "confirmar a chave") {
		t.Fatal(err)
	}
	c = base
	c.Endereco = "999.0.0.1:0"
	if err := run(ctx, c, io.Discard, nil); err == nil || !strings.Contains(err.Error(), "escutar") {
		t.Fatal(err)
	}
	// O webhook não chega.
	if err := run(ctx, base, io.Discard, nil); err == nil || !strings.Contains(err.Error(), "não chegou") {
		t.Fatal(err)
	}
	// O webhook chega, mas a sessão não está paga.
	c = base
	c.Espera = 5 * time.Second
	err := run(ctx, c, io.Discard, func(s *spolly.CheckoutSession, url string) { enviar(t, url, evento("evt_1", s.ID), segredo) })
	if err == nil || !strings.Contains(err.Error(), "está open") {
		t.Fatal(err)
	}
	// A API recusa a sessão, e depois a leitura.
	recusa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/conta" {
			io.WriteString(w, `{"merchant_id":"mer_1","nome_comercial":"Kiala","modo":"test"}`)
			return
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(201)
			io.WriteString(w, sessao(false))
			return
		}
		w.WriteHeader(404)
		io.WriteString(w, `{"erro":"sessao_nao_existe","mensagem":"Não há."}`)
	}))
	defer recusa.Close()
	c.API = recusa.URL
	err = run(ctx, c, io.Discard, func(s *spolly.CheckoutSession, url string) { enviar(t, url, evento("evt_1", s.ID), segredo) })
	if err == nil || !strings.Contains(err.Error(), "ler a sessão") {
		t.Fatal(err)
	}
	semSessao := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/conta" {
			io.WriteString(w, `{"merchant_id":"mer_1","nome_comercial":"Kiala","modo":"test"}`)
			return
		}
		w.WriteHeader(422)
		io.WriteString(w, `{"erro":"moeda_sem_gateway","mensagem":"Sem gateway."}`)
	}))
	defer semSessao.Close()
	c.API = semSessao.URL
	if err := run(ctx, c, io.Discard, nil); spolly.ErrorCode(err) != spolly.CodeCurrencyWithoutGateway {
		t.Fatal(err)
	}
}

func TestEnvOu(t *testing.T) {
	t.Setenv("SPOLLY_API_URL", "")
	if envOu("SPOLLY_API_URL", "x") != "x" {
		t.Fatal()
	}
	t.Setenv("SPOLLY_API_URL", "http://localhost:8084")
	if envOu("SPOLLY_API_URL", "x") != "http://localhost:8084" {
		t.Fatal()
	}
}
