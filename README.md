# spolly-payments-go

O SDK em Go do [Spolly Payments](https://github.com/spolly-ao/spolly-payments):
criar e consultar sessões de checkout, estornar, e verificar a assinatura dos
webhooks.

> **Ainda sem código.** O SDK é o cartão 10.1 do quadro, e nasce depois de a API
> ter as rotas que ele chama (Fases 4 a 8). Até lá este repositório guarda o
> lugar, o nome do módulo e as regras.

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
