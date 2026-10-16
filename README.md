# spolly-payments-go

O SDK em Go do [Spolly Payments](https://github.com/spolly-ao/spolly-payments):
criar e consultar sessões de checkout, estornar, e verificar a assinatura dos
webhooks.

> **Já tem a verificação dos webhooks** (pacote `webhook`). O cliente da API
> (sessões, estornos) é o resto do cartão 10.1, e chega quando a API tiver as
> rotas que ele chama.

## Verificar um webhook

```go
import "github.com/spolly-ao/spolly-payments-go/webhook"

v := webhook.Verifier{Secrets: []string{os.Getenv("SPOLLY_WEBHOOK_SECRET")}}

http.HandleFunc("/webhooks/spolly", func(w http.ResponseWriter, r *http.Request) {
	evento, _, err := v.ParseRequest(r) // lê o corpo bruto e verifica Spolly-Signature
	if err != nil {
		http.Error(w, "assinatura inválida", http.StatusBadRequest)
		return
	}
	// Deduplicar por evento.ID: as retentativas e o reenvio do painel repetem-no.
	if evento.Type == webhook.EventPaymentSucceeded { /* evento.Data.Object */ }
	w.WriteHeader(http.StatusOK)
})
```

- A tolerância do tempo é de 5 minutos por omissão (`Verifier.Tolerance`), e
  não se desliga.
- Durante a rotação do segredo no painel, o cabeçalho traz duas assinaturas, e
  basta uma bater; do lado de quem recebe, `Secrets` aceita os dois.
- As recusas são `*webhook.VerificationError`, e comparam-se com `errors.Is`:
  `ErrNoHeader`, `ErrMalformedHeader`, `ErrNoSignature`,
  `ErrSignatureMismatch`, `ErrTimestampOutsideTolerance`, `ErrInvalidPayload`,
  `ErrBodyTooLarge`, `ErrNoSecret`.
- `examples/receber-webhooks` é um servidor completo:
  `go run ./examples/receber-webhooks -segredos whsec_... -porta 8099`.

## O que vai ser

- Módulo `github.com/spolly-ao/spolly-payments-go`, **sem dependências externas**
  (só a biblioteca padrão), pela mesma razão da `fllex`: é importado por serviços
  com versões diferentes de tudo.
- `Idempotency-Key` automática em todos os `POST`.
- Verificação do cabeçalho `Spolly-Signature: t=<tempo>,v1=<hmac>` com tolerância
  de tempo, contra o corpo bruto do pedido.
- Os tipos seguem os contratos do super projeto: `contratos/openapi.yaml` e
  `contratos/eventos/`.
- Exemplos como testes `Example`, que não podem mentir.

## Quem o usa primeiro

Os quatro produtos da casa, que são todos Go: o Saluto, o Spolly, o RotaLar e o
Aduela (cartões 13.1 a 13.4).
