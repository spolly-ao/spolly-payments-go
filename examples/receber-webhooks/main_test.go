package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spolly-ao/spolly-payments-go/webhook"
)

func TestRecebeVerificaEContaAsRececoes(t *testing.T) {
	var saida bytes.Buffer
	h := novo(webhook.Verifier{Secrets: []string{"whsec_certo"}}, &saida)
	corpo := []byte(`{"id":"evt_1","type":"payment.succeeded","created_at":"2026-10-16T10:00:00Z","mode":"test","merchant_id":"mer_1","api_version":"2026-10-16","data":{"object":{"id":"pay_1","status":"succeeded","amount":{"amount":"1500000","currency":"AOA"}}}}`)
	enviar := func(segredo string) int {
		r := httptest.NewRequest(http.MethodPost, "/webhooks/spolly", bytes.NewReader(corpo))
		r.Header.Set(webhook.SignatureHeader, webhook.Sign(time.Now(), corpo, segredo))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if enviar("whsec_certo") != 200 || enviar("whsec_certo") != 200 || enviar("whsec_errado") != 400 {
		t.Fatal(saida.String())
	}
	s := saida.String()
	for _, want := range []string{"receção n.º 1 (pay_1 succeeded, 1500000 AOA)", "receção n.º 2", "RECUSADO pelo SDK: webhook: nenhuma assinatura bate"} {
		if !strings.Contains(s, want) {
			t.Errorf("falta %q em:\n%s", want, s)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal(w.Code)
	}
}
