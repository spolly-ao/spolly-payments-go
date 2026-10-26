package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const segredo = "whsec_9mLw2TcQe4XbN7aVfKp3Rz"

var corpo = []byte(`{"id":"evt_8fJ2kLmN4pQ6rS9tV1wX3y","type":"payment.succeeded","created_at":"2026-10-16T10:00:00Z","mode":"test","merchant_id":"mer_5hT7uW9yA2bC4dE6fG8hJ1","api_version":"2026-10-16","data":{"object":{"id":"pay_1","object":"payment","amount":{"amount":1500000,"currency":"AOA"}}}}`)

var agora = time.Unix(1792144800, 0)

func relogio() time.Time { return agora }

// A assinatura calculada à mão, como a documentação a descreve, e não pelo
// código do pacote: se o pacote e a documentação divergirem, este teste diz.
func aMao(s string, t int64, c []byte) string {
	m := hmac.New(sha256.New, []byte(s))
	fmt.Fprintf(m, "%d.%s", t, c)
	return hex.EncodeToString(m.Sum(nil))
}

func fmtInt(n int64) string { return strconv.FormatInt(n, 10) }

func TestUmaAssinaturaDoSpollyPaymentsBate(t *testing.T) {
	cab := "t=" + fmtInt(agora.Unix()) + ",v1=" + aMao(segredo, agora.Unix(), corpo)
	if cab != Sign(agora, corpo, segredo) {
		t.Fatalf("Sign difere da documentação:\n%s\n%s", cab, Sign(agora, corpo, segredo))
	}
	v := Verifier{Secrets: []string{segredo}, Now: relogio}
	if err := v.Verify(corpo, cab); err != nil {
		t.Fatal(err)
	}
	e, err := v.ConstructEvent(corpo, cab)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "evt_8fJ2kLmN4pQ6rS9tV1wX3y" || e.Type != EventPaymentSucceeded || e.Mode != "test" ||
		e.MerchantID != "mer_5hT7uW9yA2bC4dE6fG8hJ1" || e.APIVersion != "2026-10-16" ||
		!e.CreatedAt.Equal(time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)) || !bytes.Contains(e.Data.Object, []byte(`"pay_1"`)) {
		t.Fatalf("envelope: %+v", e)
	}
}

// A mesma assinatura que a API do Spolly Payments calcula
// (api/internal/endpoints/assinatura.go), com valores fixos.
func TestOValorBateComODaAPI(t *testing.T) {
	got := Sign(time.Unix(1700000000, 0), []byte(`{"id":"evt_1"}`), "whsec_abc")
	want := "t=1700000000,v1=" + aMao("whsec_abc", 1700000000, []byte(`{"id":"evt_1"}`))
	if got != want {
		t.Fatalf("%s != %s", got, want)
	}
}

func TestNaRotacaoBastaUmaBater(t *testing.T) {
	novo, antigo := "whsec_novo", "whsec_antigo"
	cab := Sign(agora, corpo, novo, antigo)
	for _, s := range [][]string{{novo}, {antigo}, {"whsec_outro", antigo}, {"", novo}} {
		if err := (Verifier{Secrets: s, Now: relogio}).Verify(corpo, cab); err != nil {
			t.Errorf("%v: %v", s, err)
		}
	}
	if err := (Verifier{Secrets: []string{"whsec_outro"}, Now: relogio}).Verify(corpo, cab); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("segredo errado: %v", err)
	}
}

func TestAsRecusas(t *testing.T) {
	bom := Sign(agora, corpo, segredo)
	hexa := strings.Repeat("ab", 32)
	casos := []struct {
		nome      string
		corpo     []byte
		cab       string
		segredos  []string
		razao     error
		comDetail bool
	}{
		{"sem segredos", corpo, bom, nil, ErrNoSecret, false},
		{"segredos vazios", corpo, bom, []string{""}, ErrNoSecret, false},
		{"sem cabeçalho", corpo, "", []string{segredo}, ErrNoHeader, false},
		{"cabeçalho em branco", corpo, "  ", []string{segredo}, ErrNoHeader, false},
		{"parte sem igual", corpo, "t=1,v1", []string{segredo}, ErrMalformedHeader, true},
		{"t não é número", corpo, "t=ontem,v1=" + hexa, []string{segredo}, ErrMalformedHeader, true},
		{"t repetido", corpo, "t=1,t=2,v1=" + hexa, []string{segredo}, ErrMalformedHeader, true},
		{"sem t", corpo, "v1=" + hexa, []string{segredo}, ErrMalformedHeader, true},
		{"v1 não é hexadecimal", corpo, "t=1,v1=zz", []string{segredo}, ErrMalformedHeader, true},
		{"v1 curto", corpo, "t=1,v1=abcd", []string{segredo}, ErrMalformedHeader, true},
		{"sem v1", corpo, "t=1,v0=" + hexa, []string{segredo}, ErrNoSignature, false},
		{"corpo mudado", append([]byte(" "), corpo...), bom, []string{segredo}, ErrSignatureMismatch, true},
		{"assinatura doutro t", corpo, strings.Replace(bom, "t="+fmtInt(agora.Unix()), "t="+fmtInt(agora.Unix()+1), 1), []string{segredo}, ErrSignatureMismatch, true},
	}
	for _, c := range casos {
		err := (Verifier{Secrets: c.segredos, Now: relogio}).Verify(c.corpo, c.cab)
		var ve *VerificationError
		if !errors.Is(err, c.razao) || !errors.As(err, &ve) || ve.Reason != c.razao {
			t.Errorf("%s: %v", c.nome, err)
			continue
		}
		if c.comDetail != (ve.Detail != "") || !strings.HasPrefix(err.Error(), c.razao.Error()) {
			t.Errorf("%s: pormenor %q, mensagem %q", c.nome, ve.Detail, err.Error())
		}
	}
}

func TestUmaChaveDesconhecidaIgnoraSe(t *testing.T) {
	cab := Sign(agora, corpo, segredo) + ",v2=qualquer-coisa"
	if err := (Verifier{Secrets: []string{segredo}, Now: relogio}).Verify(corpo, cab); err != nil {
		t.Fatal(err)
	}
}

func TestATolerancia(t *testing.T) {
	v := Verifier{Secrets: []string{segredo}, Now: relogio}
	for _, d := range []time.Duration{0, 5 * time.Minute, -5 * time.Minute} {
		if err := v.Verify(corpo, Sign(agora.Add(d), corpo, segredo)); err != nil {
			t.Errorf("%s: %v", d, err)
		}
	}
	for _, d := range []time.Duration{5*time.Minute + time.Second, -5*time.Minute - time.Second, 24 * time.Hour} {
		err := v.Verify(corpo, Sign(agora.Add(-d), corpo, segredo))
		if !errors.Is(err, ErrTimestampOutsideTolerance) || !strings.Contains(err.Error(), "tolerância é 5m0s") {
			t.Errorf("%s: %v", d, err)
		}
	}
	// Uma tolerância própria; zero e negativo são a por omissão.
	v.Tolerance = time.Hour
	if err := v.Verify(corpo, Sign(agora.Add(-50*time.Minute), corpo, segredo)); err != nil {
		t.Fatal(err)
	}
	v.Tolerance = -time.Hour
	if err := v.Verify(corpo, Sign(agora.Add(-50*time.Minute), corpo, segredo)); !errors.Is(err, ErrTimestampOutsideTolerance) {
		t.Fatalf("tolerância negativa: %v", err)
	}
}

func TestSemRelogioUsaOTempoReal(t *testing.T) {
	if err := Verify(corpo, Sign(time.Now(), corpo, segredo), segredo); err != nil {
		t.Fatal(err)
	}
	if _, err := ConstructEvent(corpo, Sign(time.Now(), corpo, segredo), segredo); err != nil {
		t.Fatal(err)
	}
	if err := Verify(corpo, Sign(time.Now().Add(-time.Hour), corpo, segredo), segredo); !errors.Is(err, ErrTimestampOutsideTolerance) {
		t.Fatal(err)
	}
}

func TestConstructEventRecusaOQueNaoEEnvelope(t *testing.T) {
	v := Verifier{Secrets: []string{segredo}, Now: relogio}
	for _, c := range []string{`não é json`, `{"id":"pay_1","type":"payment.succeeded"}`, `{"id":"evt_1"}`, `[]`} {
		_, err := v.ConstructEvent([]byte(c), Sign(agora, []byte(c), segredo))
		if !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("%s: %v", c, err)
		}
	}
	if _, err := v.ConstructEvent(corpo, "t=1"); !errors.Is(err, ErrNoSignature) {
		t.Fatal(err)
	}
}

type leitorAvariado struct{}

func (leitorAvariado) Read([]byte) (int, error) { return 0, errors.New("ligação cortada") }

func TestParseRequest(t *testing.T) {
	v := Verifier{Secrets: []string{segredo}, Now: relogio}
	r := httptest.NewRequest(http.MethodPost, "/webhooks/spolly", bytes.NewReader(corpo))
	r.Header.Set(SignatureHeader, Sign(agora, corpo, segredo))
	e, bruto, err := v.ParseRequest(r)
	if err != nil || e.ID != "evt_8fJ2kLmN4pQ6rS9tV1wX3y" || !bytes.Equal(bruto, corpo) {
		t.Fatalf("%v %+v", err, e)
	}

	r = httptest.NewRequest(http.MethodPost, "/webhooks/spolly", bytes.NewReader(corpo))
	if _, _, err := v.ParseRequest(r); !errors.Is(err, ErrNoHeader) {
		t.Fatal(err)
	}

	grande := bytes.Repeat([]byte("a"), MaxBodyBytes+1)
	r = httptest.NewRequest(http.MethodPost, "/webhooks/spolly", bytes.NewReader(grande))
	r.Header.Set(SignatureHeader, Sign(agora, grande, segredo))
	if _, _, err := v.ParseRequest(r); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatal(err)
	}

	r = httptest.NewRequest(http.MethodPost, "/webhooks/spolly", leitorAvariado{})
	if _, _, err := v.ParseRequest(r); err == nil || !strings.Contains(err.Error(), "ligação cortada") {
		t.Fatal(err)
	}
}

// Os eventos de uma conta ligada chegam à plataforma com account no
// envelope; os do próprio comerciante não o têm.
func TestOEventoDeUmaContaLigadaDizDeQueContaVem(t *testing.T) {
	v := Verifier{Secrets: []string{segredo}, Now: relogio}
	ligada := []byte(`{"id":"evt_01JA9X8Y8A","type":"payout.created","created_at":"2026-11-30T10:15:00Z","mode":"live",` +
		`"merchant_id":"mer_01JA9BILHETEIRA","application":"app_01JA9BILHETES","account":"mer_01JA9VENDEDOR","api_version":"2026-10-16",` +
		`"data":{"object":{"id":"po_01JA9P","object":"payout","amount":{"amount":2500000,"currency":"AOA"},"status":"pending","created_at":"2026-11-30T10:15:00Z"}}}`)
	e, err := v.ConstructEvent(ligada, Sign(agora, ligada, segredo))
	if err != nil {
		t.Fatal(err)
	}
	if e.Account != "mer_01JA9VENDEDOR" || e.MerchantID != "mer_01JA9BILHETEIRA" || e.Type != EventPayoutCreated || *e.Application != "app_01JA9BILHETES" {
		t.Fatalf("%+v", e)
	}
	e, err = v.ConstructEvent(corpo, Sign(agora, corpo, segredo))
	if err != nil || e.Account != "" {
		t.Fatalf("%+v %v", e, err)
	}
}

// Os tipos de evento são os do enum de contratos/eventos/envelope.schema.json.
func TestOsTiposDeEventoSaoOsDoContrato(t *testing.T) {
	contrato := []string{"account.updated", "balance.available", "checkout.session.expired", "payment.failed", "payment.held",
		"payment.released", "payment.succeeded", "payout.created", "payout.failed", "payout.paid", "payout.processing",
		"refund.created", "subscription.cancelled", "subscription.created", "subscription.past_due", "subscription.renewed"}
	sdk := []string{EventAccountUpdated, EventBalanceAvailable, EventCheckoutSessionExpired, EventPaymentFailed, EventPaymentHeld,
		EventPaymentReleased, EventPaymentSucceeded, EventPayoutCreated, EventPayoutFailed, EventPayoutPaid, EventPayoutProcessing,
		EventRefundCreated, EventSubscriptionCancelled, EventSubscriptionCreated, EventSubscriptionPastDue, EventSubscriptionRenewed}
	if strings.Join(contrato, ",") != strings.Join(sdk, ",") {
		t.Fatalf("\n%v\n%v", contrato, sdk)
	}
}
