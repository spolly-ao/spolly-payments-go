// Package webhook verifica a assinatura dos webhooks do Spolly Payments e lê o
// envelope de cada evento.
//
// Cada webhook chega com o cabeçalho Spolly-Signature:
//
//	Spolly-Signature: t=<unix>,v1=<hex(hmac_sha256(segredo, "<t>.<corpo>"))>[,v1=<...>]
//
// A chave do HMAC são os bytes do segredo inteiro, com o prefixo whsec_.
// Durante a rotação de um segredo (24 horas) o cabeçalho leva duas
// assinaturas v1, uma por segredo, e basta uma bater. O tempo t entra na
// assinatura para se recusar um evento repetido muito depois (ataque de
// repetição): por omissão, aceita-se até 5 minutos de diferença para o relógio
// de quem recebe.
//
// A verificação faz-se sobre o corpo bruto, tal como chegou. Um JSON lido e
// escrito outra vez muda espaços e ordem, e a assinatura deixa de bater.
//
// A mesma entrega pode chegar mais de uma vez (as retentativas, e o reenvio
// manual no painel levam o mesmo id): deduplica-se pelo Event.ID.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Os cabeçalhos de cada webhook.
const (
	SignatureHeader = "Spolly-Signature"
	EventIDHeader   = "Spolly-Event-Id"
	EventTypeHeader = "Spolly-Event-Type"
)

// DefaultTolerance é a diferença máxima, por omissão, entre o t da assinatura
// e o relógio de quem recebe.
const DefaultTolerance = 5 * time.Minute

// MaxBodyBytes é o maior corpo que ParseRequest lê. Os envelopes do Spolly
// Payments ficam muito abaixo disto.
const MaxBodyBytes = 1 << 20

// Os tipos de evento (contratos/eventos/envelope.schema.json).
const (
	EventCheckoutSessionExpired = "checkout.session.expired"
	EventPaymentFailed          = "payment.failed"
	EventPaymentSucceeded       = "payment.succeeded"
	EventPayoutFailed           = "payout.failed"
	EventPayoutPaid             = "payout.paid"
	EventRefundCreated          = "refund.created"
	EventSubscriptionCancelled  = "subscription.cancelled"
	EventSubscriptionCreated    = "subscription.created"
	EventSubscriptionPastDue    = "subscription.past_due"
	EventSubscriptionRenewed    = "subscription.renewed"
)

// As razões de uma recusa. Comparam-se com errors.Is; o erro devolvido é um
// *VerificationError com o pormenor.
var (
	// ErrNoSecret: não se deu nenhum segredo com que verificar.
	ErrNoSecret = errors.New("webhook: nenhum segredo com que verificar")
	// ErrNoHeader: o pedido não trouxe o cabeçalho Spolly-Signature.
	ErrNoHeader = errors.New("webhook: falta o cabeçalho Spolly-Signature")
	// ErrMalformedHeader: o cabeçalho não tem a forma t=<unix>,v1=<hex>.
	ErrMalformedHeader = errors.New("webhook: o cabeçalho Spolly-Signature está mal formado")
	// ErrNoSignature: o cabeçalho não traz nenhuma assinatura v1.
	ErrNoSignature = errors.New("webhook: o cabeçalho Spolly-Signature não traz nenhuma assinatura v1")
	// ErrSignatureMismatch: nenhuma assinatura v1 bate com os segredos dados.
	ErrSignatureMismatch = errors.New("webhook: nenhuma assinatura bate com os segredos dados")
	// ErrTimestampOutsideTolerance: a assinatura bate, mas o t está longe de
	// mais do relógio (um evento repetido, ou um relógio errado).
	ErrTimestampOutsideTolerance = errors.New("webhook: o tempo da assinatura está fora da tolerância")
	// ErrInvalidPayload: o corpo não é um envelope de evento.
	ErrInvalidPayload = errors.New("webhook: o corpo não é um envelope de evento")
	// ErrBodyTooLarge: o corpo passa MaxBodyBytes.
	ErrBodyTooLarge = errors.New("webhook: o corpo é grande de mais")
)

// VerificationError é o erro de todas as recusas: Reason é uma das variáveis
// Err* deste pacote, e Detail diz o que se encontrou.
type VerificationError struct {
	Reason error
	Detail string
}

func (e *VerificationError) Error() string {
	if e.Detail == "" {
		return e.Reason.Error()
	}
	return e.Reason.Error() + ": " + e.Detail
}

// Unwrap deixa errors.Is(err, ErrSignatureMismatch) funcionar.
func (e *VerificationError) Unwrap() error { return e.Reason }

func recusa(razao error, formato string, args ...any) error {
	return &VerificationError{Reason: razao, Detail: fmt.Sprintf(formato, args...)}
}

// Event é o envelope de um evento.
type Event struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	CreatedAt  time.Time `json:"created_at"`
	Mode       string    `json:"mode"`
	MerchantID string    `json:"merchant_id"`
	APIVersion string    `json:"api_version"`
	Data       EventData `json:"data"`
}

// EventData leva o objeto do evento, em bruto: lê-se para o tipo certo pelo
// Event.Type (um pagamento, um estorno, um saque, uma subscrição).
type EventData struct {
	Object json.RawMessage `json:"object"`
}

// Verifier verifica com uma lista de segredos (o atual e, durante uma
// rotação feita do lado de quem recebe, o anterior).
type Verifier struct {
	// Secrets são os segredos whsec_ aceites. Basta um bater.
	Secrets []string
	// Tolerance é a diferença máxima entre o t e o relógio. Zero, ou
	// negativo, é DefaultTolerance: não há forma de desligar a verificação
	// do tempo.
	Tolerance time.Duration
	// Now é o relógio. Nil é time.Now.
	Now func() time.Time
}

// Verify verifica a assinatura com a tolerância por omissão.
func Verify(payload []byte, header string, secrets ...string) error {
	return Verifier{Secrets: secrets}.Verify(payload, header)
}

// ConstructEvent verifica a assinatura com a tolerância por omissão e devolve
// o envelope.
func ConstructEvent(payload []byte, header string, secrets ...string) (Event, error) {
	return Verifier{Secrets: secrets}.ConstructEvent(payload, header)
}

// Verify devolve nil se uma das assinaturas v1 do cabeçalho bater com um dos
// segredos, sobre o corpo bruto, e o t estiver dentro da tolerância. A
// comparação é em tempo constante.
func (v Verifier) Verify(payload []byte, header string) error {
	segredos := make([]string, 0, len(v.Secrets))
	for _, s := range v.Secrets {
		if s != "" {
			segredos = append(segredos, s)
		}
	}
	if len(segredos) == 0 {
		return &VerificationError{Reason: ErrNoSecret}
	}
	if strings.TrimSpace(header) == "" {
		return &VerificationError{Reason: ErrNoHeader}
	}
	t, assinaturas, err := lerCabecalho(header)
	if err != nil {
		return err
	}
	if !bate(segredos, t, payload, assinaturas) {
		return recusa(ErrSignatureMismatch, "%d assinatura(s) v1 e %d segredo(s)", len(assinaturas), len(segredos))
	}
	tolerancia := v.Tolerance
	if tolerancia <= 0 {
		tolerancia = DefaultTolerance
	}
	agora := time.Now
	if v.Now != nil {
		agora = v.Now
	}
	quando := time.Unix(t, 0)
	if d := agora().Sub(quando); d > tolerancia || d < -tolerancia {
		return recusa(ErrTimestampOutsideTolerance, "assinado a %s, %s de diferença, e a tolerância é %s",
			quando.UTC().Format(time.RFC3339), d.Round(time.Second), tolerancia)
	}
	return nil
}

// ConstructEvent verifica a assinatura e lê o envelope.
func (v Verifier) ConstructEvent(payload []byte, header string) (Event, error) {
	if err := v.Verify(payload, header); err != nil {
		return Event{}, err
	}
	var e Event
	if err := json.Unmarshal(payload, &e); err != nil {
		return Event{}, recusa(ErrInvalidPayload, "%v", err)
	}
	if !strings.HasPrefix(e.ID, "evt_") || e.Type == "" {
		return Event{}, recusa(ErrInvalidPayload, "sem id evt_ ou sem type")
	}
	return e, nil
}

// ParseRequest lê o corpo do pedido (até MaxBodyBytes), verifica a
// assinatura do cabeçalho Spolly-Signature e devolve o envelope e o corpo
// bruto.
func (v Verifier) ParseRequest(r *http.Request) (Event, []byte, error) {
	corpo, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		return Event{}, nil, fmt.Errorf("webhook: ler o corpo: %w", err)
	}
	if len(corpo) > MaxBodyBytes {
		return Event{}, nil, recusa(ErrBodyTooLarge, "mais de %d bytes", MaxBodyBytes)
	}
	e, err := v.ConstructEvent(corpo, r.Header.Get(SignatureHeader))
	return e, corpo, err
}

// lerCabecalho lê t=<unix>,v1=<hex>[,v1=<hex>]. Chaves desconhecidas (um
// esquema futuro, v2) ignoram-se; um t repetido é mal formado.
func lerCabecalho(header string) (int64, [][]byte, error) {
	var (
		t           int64
		temT        bool
		assinaturas [][]byte
	)
	for _, parte := range strings.Split(header, ",") {
		k, val, ok := strings.Cut(strings.TrimSpace(parte), "=")
		if !ok {
			return 0, nil, recusa(ErrMalformedHeader, "a parte %q não é chave=valor", parte)
		}
		switch k {
		case "t":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil || temT {
				return 0, nil, recusa(ErrMalformedHeader, "o t tem de aparecer uma vez, em segundos desde 1970")
			}
			t, temT = n, true
		case "v1":
			b, err := hex.DecodeString(val)
			if err != nil || len(b) != sha256.Size {
				return 0, nil, recusa(ErrMalformedHeader, "uma assinatura v1 não é um HMAC-SHA256 em hexadecimal")
			}
			assinaturas = append(assinaturas, b)
		}
	}
	if !temT {
		return 0, nil, recusa(ErrMalformedHeader, "falta o t")
	}
	if len(assinaturas) == 0 {
		return 0, nil, &VerificationError{Reason: ErrNoSignature}
	}
	return t, assinaturas, nil
}

func calcular(segredo string, t int64, corpo []byte) []byte {
	m := hmac.New(sha256.New, []byte(segredo))
	m.Write([]byte(strconv.FormatInt(t, 10)))
	m.Write([]byte{'.'})
	m.Write(corpo)
	return m.Sum(nil)
}

// bate compara todas as combinações, sem parar na primeira que bate, para o
// tempo da resposta não dizer qual bateu.
func bate(segredos []string, t int64, corpo []byte, assinaturas [][]byte) bool {
	ok := false
	for _, s := range segredos {
		esperada := calcular(s, t, corpo)
		for _, a := range assinaturas {
			if hmac.Equal(a, esperada) {
				ok = true
			}
		}
	}
	return ok
}

// Sign devolve o valor do cabeçalho Spolly-Signature para o corpo, com uma
// assinatura v1 por segredo, como o Spolly Payments o envia. Serve para os
// testes de quem integra.
func Sign(t time.Time, payload []byte, secrets ...string) string {
	partes := []string{"t=" + strconv.FormatInt(t.Unix(), 10)}
	for _, s := range secrets {
		partes = append(partes, "v1="+hex.EncodeToString(calcular(s, t.Unix(), payload)))
	}
	return strings.Join(partes, ",")
}
