package spolly

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Os códigos de erro da API (o campo erro do corpo). São estáveis: o programa
// decide por eles, e mostra a Message a uma pessoa.
const (
	// 400
	CodeInvalidRequest     = "pedido_invalido"
	CodeIdempotencyMissing = "idempotencia_em_falta"
	CodeIdempotencyInvalid = "idempotencia_invalida"
	// 401
	CodeNoKey      = "sem_chave"
	CodeInvalidKey = "chave_invalida"
	// 404
	CodeSessionNotFound = "sessao_nao_existe"
	CodePaymentNotFound = "pagamento_nao_existe"
	CodeRefundNotFound  = "estorno_nao_existe"
	// CodeSubscriptionNotFound: não há subscrição com este id para esta chave.
	CodeSubscriptionNotFound = "subscricao_nao_existe"
	// 409
	CodePaymentNotPaid         = "pagamento_nao_pago"
	CodePaymentAlreadyRefunded = "pagamento_ja_estornado"
	// CodeSubscriptionEnded: a subscrição já terminou (cancelar, ou abrir a
	// página do cliente).
	CodeSubscriptionEnded = "subscricao_terminada"
	// 422
	CodeInvalidData            = "dados_invalidos"
	CodeIdempotencyReused      = "idempotencia_reutilizada"
	CodeCurrencyWithoutGateway = "moeda_sem_gateway"
	CodeMethodWithoutGateway   = "metodo_sem_gateway"
	CodeRefundExceedsCharged   = "estorno_acima_do_cobrado"
	// 500 e 503
	CodeInternal    = "erro_interno"
	CodeUnavailable = "indisponivel"
	// CodeUnreadableError é o código que o SDK põe quando a API respondeu
	// com um erro cujo corpo não se conseguiu ler (um proxy pelo meio, por
	// exemplo). O StatusCode continua a ser o que veio.
	CodeUnreadableError = "resposta_ilegivel"
)

// Error é uma recusa da API: o estado HTTP, o código estável, a mensagem em
// português, e, no 422 dados_invalidos, uma mensagem por campo.
type Error struct {
	// StatusCode é o estado HTTP da resposta.
	StatusCode int
	// Code é o código estável (um dos Code*), para o programa decidir.
	Code string
	// Message é a explicação em português, para mostrar a uma pessoa.
	Message string
	// Fields tem uma mensagem por campo, só no 422 dados_invalidos.
	Fields map[string]string
	// IdempotencyKey é a chave com que o POST foi feito; repetir com ela é
	// seguro.
	IdempotencyKey string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "spolly: %d %s: %s", e.StatusCode, e.Code, e.Message)
	for _, k := range slices.Sorted(maps.Keys(e.Fields)) {
		fmt.Fprintf(&b, "; %s: %s", k, e.Fields[k])
	}
	return b.String()
}

// Is deixa comparar pelo código: errors.Is(err, &spolly.Error{Code:
// spolly.CodeRefundExceedsCharged}). Um alvo sem código compara pelo estado.
func (e *Error) Is(alvo error) bool {
	var a *Error
	if !errors.As(alvo, &a) {
		return false
	}
	if a.Code != "" {
		return a.Code == e.Code
	}
	return a.StatusCode != 0 && a.StatusCode == e.StatusCode
}

// ErrorCode devolve o código de um *Error, ou "" se err não vier da API.
func ErrorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func lerErro(estado int, corpo []byte, chave string) error {
	var c struct {
		Erro     string            `json:"erro"`
		Mensagem string            `json:"mensagem"`
		Campos   map[string]string `json:"campos"`
	}
	if err := json.Unmarshal(corpo, &c); err != nil || c.Erro == "" {
		resumo := strings.TrimSpace(string(corpo))
		if len(resumo) > 200 {
			resumo = resumo[:200] + "..."
		}
		return &Error{StatusCode: estado, Code: CodeUnreadableError,
			Message: fmt.Sprintf("a API respondeu %d com um corpo que não é um erro dela: %q", estado, resumo), IdempotencyKey: chave}
	}
	return &Error{StatusCode: estado, Code: c.Erro, Message: c.Mensagem, Fields: c.Campos, IdempotencyKey: chave}
}
