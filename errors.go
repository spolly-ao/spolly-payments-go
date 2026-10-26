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
	// CodeAccountHeaderNotAllowed: o cabeçalho Spolly-Account numa rota das
	// contas ligadas, onde a conta já vai no caminho. O cliente nunca o manda
	// lá; só aparece a quem monte o pedido à mão.
	CodeAccountHeaderNotAllowed = "spolly_account_nao_serve_aqui"
	// 401
	CodeNoKey      = "sem_chave"
	CodeInvalidKey = "chave_invalida"
	// 403
	// CodePlatformNotAuthorized: a sessão pede PlatformFee, e o comerciante
	// da chave não autorizou essa plataforma.
	CodePlatformNotAuthorized = "plataforma_nao_autorizada"
	// CodeConnectedAccountsNotEnabled: a conta da chave não tem as contas
	// ligadas habilitadas pela equipa.
	CodeConnectedAccountsNotEnabled = "contas_ligadas_nao_habilitadas"
	// CodeAccountOtherMode: a conta de WithAccount foi criada com uma chave
	// do outro modo (teste ou produção).
	CodeAccountOtherMode = "conta_de_outro_modo"
	// CodeAccountNotVerified: em produção, uma escrita em nome de uma conta
	// ligada por verificar, ou um saque dela (409 neste caso).
	CodeAccountNotVerified = "conta_por_verificar"
	// CodePayoutRefused: o saque não pode ser feito.
	CodePayoutRefused = "saque_recusado"
	// 404
	CodeSessionNotFound = "sessao_nao_existe"
	CodePaymentNotFound = "pagamento_nao_existe"
	CodeRefundNotFound  = "estorno_nao_existe"
	// CodeSubscriptionNotFound: não há subscrição com este id para esta chave.
	CodeSubscriptionNotFound = "subscricao_nao_existe"
	// CodeAccountNotFound: a conta ligada não existe nesta plataforma, neste
	// modo (no caminho, ou em WithAccount).
	CodeAccountNotFound = "conta_nao_existe"
	// CodePayoutNotFound: não há saque com este id nesta conta.
	CodePayoutNotFound = "saque_nao_existe"
	// 409
	CodePaymentNotPaid         = "pagamento_nao_pago"
	CodePaymentAlreadyRefunded = "pagamento_ja_estornado"
	// CodePaidToMerchantAccount: o pagamento foi uma transferência bancária
	// direta para a conta do comerciante; não se estorna pela API, a devolução
	// faz-se dessa conta.
	CodePaidToMerchantAccount = "pago_na_conta_do_comerciante"
	// CodeSubscriptionEnded: a subscrição já terminou (cancelar, ou abrir a
	// página do cliente).
	CodeSubscriptionEnded = "subscricao_terminada"
	// CodeReferenceRepeated: já há uma conta ligada desta plataforma, neste
	// modo, com esta Reference.
	CodeReferenceRepeated = "referencia_repetida"
	// CodeVerificationLocked: a verificação da conta ligada, em análise ou
	// aprovada, não deixa fazer a alteração (o nome legal, o NIF, um
	// documento, a conta bancária).
	CodeVerificationLocked = "verificacao_nao_deixa"
	// CodeTooManyDocuments: a conta ligada já tem o máximo de documentos.
	CodeTooManyDocuments = "documentos_demais"
	// CodeBankAccountNotVerified: a conta bancária da conta ligada está por
	// verificar, e não há saques até lá.
	CodeBankAccountNotVerified = "conta_bancaria_por_verificar"
	// CodeNegativeBalance: o saldo da conta é negativo, e não se saca.
	CodeNegativeBalance = "saldo_negativo"
	// CodeInsufficientBalance: o saque passa o saldo disponível.
	CodeInsufficientBalance = "saldo_insuficiente"
	// 422
	CodeInvalidData            = "dados_invalidos"
	CodeIdempotencyReused      = "idempotencia_reutilizada"
	CodeCurrencyWithoutGateway = "moeda_sem_gateway"
	CodeMethodWithoutGateway   = "metodo_sem_gateway"
	CodeRefundExceedsCharged   = "estorno_acima_do_cobrado"
	// CodePlatformFeeAboveMaximum: a comissão da plataforma passa o máximo
	// que o comerciante autorizou.
	CodePlatformFeeAboveMaximum = "comissao_acima_do_maximo"
	// CodePlatformFeeInvalid: a comissão é zero, negativa, da própria
	// plataforma, deixava o comerciante sem líquido, ou vinha com a
	// transferência bancária em PaymentMethods (que vai direta para a conta do
	// comerciante).
	CodePlatformFeeInvalid = "comissao_invalida"
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
