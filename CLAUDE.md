# spolly-payments-go: instruções

O SDK em Go do Spolly Payments. É o submódulo `sdk-go/` do super projeto
`spolly-payments`; as regras da raiz (`../CLAUDE.md`, `../.claude/rules/`) valem
aqui.

## Estado

O estado do trabalho vive no super projeto `spolly-payments`, que liga este repositório por submódulo. Este repositório é público: aqui não se fala em cartões, em clientes, nem em caminhos de máquinas.

| Pasta | O que é |
|---|---|
| `.` (pacote `spolly`) | O cliente da API: `NewClient` e as opções, `CreateCheckoutSession`, `GetCheckoutSession`, `CreateRefund`, `GetRefund`, `GetAccount`, `GetSubscription`, `CancelSubscription`, `CreateSubscriptionPortalLink`; `Idempotency-Key` automática (a mesma nas retentativas), retentativas em rede, 429 e 5xx com `Retry-After`, tempo limite por tentativa; `*Error` com o código estável da API; `ErrInvalidResponse` em vez de um objeto vazio. 100% de cobertura, e um teste `Example` por operação |
| `webhook/` | `Verifier` (segredos, `Tolerance` com 5 minutos por omissão, `Now`), `Verify`, `ConstructEvent`, `ParseRequest`, `Sign`; erros tipados (`*VerificationError` com `Reason` um dos `Err*`, por `errors.Is`); o `Event` do envelope. 100% de cobertura, e testes `Example` |
| `examples/checkout/` | O percurso inteiro: confirma a chave, cria uma sessão, espera pelo `payment.succeeded` dela (verificado com o SDK) e lê a sessão paga, contra a API e o worker a correr |
| `examples/receber-webhooks/` | Um servidor de comerciante que verifica com o pacote e conta as receções de cada `id`. Serve de servidor externo nas provas de ponta a ponta |

**Os nomes exportados são estáveis** (a documentação em `payments.spolly.ao/docs`
cita-os): mudar um nome é uma versão maior. Uma rota nova da `/v1` entra aqui no
mesmo trabalho em que entra no `contratos/openapi.yaml`, com teste e `Example`.

## Regras que valem aqui

- **Zero dependências externas.** Só a biblioteca padrão.
- **O contrato é o da raiz**: `contratos/openapi.yaml` e `contratos/eventos/`.
  Um tipo que não bata com o contrato é um defeito do SDK, não do contrato.
- **Dinheiro em unidades menores inteiras** (`int64`), nunca `float64`.
- **A verificação da assinatura** compara em tempo constante
  (`hmac.Equal`), recusa um `t` fora da tolerância, e lê o corpo bruto, nunca um
  JSON re-serializado.
- Nomes em inglês; documentação, comentários e mensagens de erro em português
  europeu, sem travessão.
- Commits só pelos scripts da raiz (`../scripts/commit.sh`).

## Comandos

```bash
gofmt -l . && go vet ./... && go test -race -cover ./...
go run ./examples/receber-webhooks -segredos whsec_... -porta 8099
SPOLLY_SECRET_KEY=sk_test_... SPOLLY_WEBHOOK_SECRETS=whsec_... go run ./examples/checkout -api http://localhost:8084 -porta 7400
```

Para usar o módulo noutro antes da etiqueta: um `go.work` fora do repositório,
nunca um `replace` commitado.
