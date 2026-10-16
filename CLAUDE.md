# spolly-payments-go: instruções

O SDK em Go do Spolly Payments. É o submódulo `sdk-go/` do super projeto
`spolly-payments`; as regras da raiz (`../CLAUDE.md`, `../.claude/rules/`) valem
aqui.

## Estado

Sem código ainda. O trabalho é o **cartão 10.1** do `TASKS-PROGRESS.md` da raiz e
do quadro no Notion. Não se escreve código aqui antes de a API ter as rotas que
ele chama: um SDK escrito contra uma API imaginada é um SDK que se reescreve.

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

## Comandos (quando houver código)

```bash
gofmt -l . && go vet ./... && go test -race ./...
```
