package spolly

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// As contas ligadas: uma plataforma habilitada pela equipa do Spolly Payments
// cria e gere pela API os vendedores que vendem por ela, e o vendedor nunca
// fala com o Spolly Payments. A plataforma entrega a verificação (o documento
// de identificação e o comprovativo do IBAN), cobra em nome da conta
// (WithAccount), lê o saldo, pede os saques, e recebe os eventos da conta nos
// webhooks dela, com webhook.Event.Account preenchido. Sem a habilitação
// ativa, todas estas operações dão *Error com CodeConnectedAccountsNotEnabled.

// Os estados da verificação de uma conta ligada (AccountVerification.Status).
const (
	// VerificationPending: falta entregar alguma coisa (Missing diz o quê).
	VerificationPending = "pending"
	// VerificationInReview: tudo entregue, à espera da equipa.
	VerificationInReview = "in_review"
	// VerificationVerified: aprovada.
	VerificationVerified = "verified"
	// VerificationRejected: recusada, com o motivo em Reason.
	VerificationRejected = "rejected"
	// VerificationMoreDocuments: a equipa pede mais documentos, com o motivo
	// em Reason.
	VerificationMoreDocuments = "more_documents"
)

// O que pode faltar para a verificação seguir sozinha para análise
// (AccountVerification.Missing).
const (
	MissingLegalName        = "legal_name"
	MissingIdentityDocument = "identity_document"
	MissingBankAccount      = "bank_account"
)

// Os tipos de documento de uma conta ligada.
const (
	// DocumentIdentity é o documento de identificação do titular (bilhete de
	// identidade ou passaporte).
	DocumentIdentity = "identity_document"
	// DocumentBankAccountProof é o comprovativo do IBAN.
	DocumentBankAccountProof = "bank_account_proof"
	// DocumentOther é um documento que a equipa juntou; não se entrega pela
	// API.
	DocumentOther = "other"
)

// MaxDocumentSize é o maior documento que se entrega: 10 MiB.
const MaxDocumentSize = 10 << 20

// Os estados da conta bancária de uma conta ligada.
const (
	BankAccountVerified = "verified"
	// BankAccountPendingVerification: um IBAN mudado depois da verificação,
	// à espera da equipa. A conta não saca até lá.
	BankAccountPendingVerification = "pending_verification"
)

// Os estados de um saque.
const (
	// PayoutPending: pedido, à espera de aprovação.
	PayoutPending = "pending"
	// PayoutProcessing: aprovado, à espera da transferência.
	PayoutProcessing = "processing"
	// PayoutPaid: a transferência está feita.
	PayoutPaid = "paid"
	// PayoutFailed: recusado; o valor voltou ao saldo disponível.
	PayoutFailed = "failed"
)

// ConnectedAccount é uma conta ligada: um vendedor da plataforma. Chama-se
// assim, e não Account, porque Account é a conta da própria chave
// (GetAccount).
type ConnectedAccount struct {
	// ID é o identificador da conta (mer_...), o que vai em WithAccount.
	ID     string `json:"id"`
	Object string `json:"object"`
	// Mode é o modo da chave que a criou: uma conta ligada vive num modo só.
	Mode Mode `json:"mode"`
	// Name é o nome que o comprador vê no checkout.
	Name string `json:"name"`
	// LegalName é o nome legal do titular, tal como no documento de
	// identificação.
	LegalName string `json:"legal_name"`
	// TaxID é o NIF, se o houver.
	TaxID string `json:"tax_id"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	// Reference é o id do vendedor do lado da plataforma, único por
	// plataforma e modo.
	Reference    string              `json:"reference"`
	Metadata     map[string]string   `json:"metadata"`
	Verification AccountVerification `json:"verification"`
	// BankAccount é a conta bancária dos saques, ou nil.
	BankAccount *AccountBankAccount `json:"bank_account"`
	Documents   []AccountDocument   `json:"documents"`
	// ChargesEnabled diz se a plataforma pode cobrar em nome da conta: com a
	// habilitação ativa, e em produção só depois da verificação.
	ChargesEnabled bool `json:"charges_enabled"`
	// PayoutsEnabled diz se a conta pode sacar: verificada, com a conta
	// bancária verificada.
	PayoutsEnabled bool      `json:"payouts_enabled"`
	CreatedAt      time.Time `json:"created_at"`
}

func (a *ConnectedAccount) valido() bool {
	return strings.HasPrefix(a.ID, "mer_") && a.Object == "account"
}

// AccountVerification é o estado da verificação de uma conta ligada.
type AccountVerification struct {
	// Status é um dos Verification*.
	Status string `json:"status"`
	// Reason é o motivo da equipa, em VerificationRejected e
	// VerificationMoreDocuments.
	Reason string `json:"reason"`
	// Missing é o que falta entregar (os Missing*) para a verificação seguir
	// sozinha para análise.
	Missing []string `json:"missing"`
}

// AccountBankAccount é a conta bancária dos saques de uma conta ligada. O
// IBAN inteiro nunca volta: só os quatro últimos carateres.
type AccountBankAccount struct {
	ID        string `json:"id"`
	IBANLast4 string `json:"iban_last4"`
	Holder    string `json:"holder"`
	// Status é BankAccountVerified ou BankAccountPendingVerification.
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// AccountDocument é um documento da verificação de uma conta ligada.
type AccountDocument struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	// Type é um dos Document*.
	Type     string `json:"type"`
	FileName string `json:"file_name"`
	// ContentType é o tipo reconhecido pelo conteúdo: application/pdf,
	// image/jpeg ou image/png.
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
}

func (d *AccountDocument) valido() bool {
	return strings.HasPrefix(d.ID, "doc_") && d.Object == "account_document"
}

// AccountParams são os dados de uma conta ligada. Em CreateAccount, o Name é
// obrigatório; em UpdateAccount, só muda o que vier, e um campo vazio não
// viaja (fica como estava). O Metadata, quando vem, substitui os metadados
// todos.
type AccountParams struct {
	// Name é o nome que o comprador vê no checkout, até 120 carateres.
	Name string `json:"name,omitempty"`
	// LegalName é o nome legal do titular, tal como no documento de
	// identificação. Só muda enquanto a verificação o deixar.
	LegalName string `json:"legal_name,omitempty"`
	// TaxID é o NIF, se o houver (9 a 14 letras e dígitos). Só muda enquanto
	// a verificação o deixar.
	TaxID string `json:"tax_id,omitempty"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
	// Reference é o id do vendedor do lado da plataforma, único por
	// plataforma e modo (repetido: CodeReferenceRepeated).
	Reference string            `json:"reference,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// ListAccountsParams filtra e pagina ListAccounts. nil é a primeira página,
// com 20 contas.
type ListAccountsParams struct {
	// Limit é quantas contas por página, de 1 a 100. Zero é 20.
	Limit int
	// StartingAfter é o ID da última conta da página anterior.
	StartingAfter string
	// Reference devolve só a conta com esta referência.
	Reference string
}

// AccountList é uma página de contas ligadas, da mais recente para a mais
// antiga.
type AccountList struct {
	Object string             `json:"object"`
	Data   []ConnectedAccount `json:"data"`
	// HasMore diz que há mais: pede-se a página seguinte com StartingAfter
	// igual ao ID da última de Data.
	HasMore bool `json:"has_more"`
}

func (l *AccountList) valido() bool {
	if l.Object != "list" || l.Data == nil {
		return false
	}
	for i := range l.Data {
		if !l.Data[i].valido() {
			return false
		}
	}
	return true
}

// AccountDocumentParams é um documento a entregar.
type AccountDocumentParams struct {
	// Type é DocumentIdentity ou DocumentBankAccountProof.
	Type string
	// FileName é o nome do ficheiro, obrigatório (por exemplo "bi.pdf").
	FileName string
	// Content é o conteúdo, em PDF, JPEG ou PNG, até MaxDocumentSize. O tipo
	// reconhece-se pelo conteúdo, e não pelo nome. Lê-se inteiro antes do
	// primeiro envio, para as retentativas mandarem os mesmos bytes.
	Content io.Reader
}

// ErrInvalidDocument é um documento que não se chega a enviar: sem
// parâmetros, sem nome de ficheiro, sem conteúdo, com um conteúdo que não se
// leu, ou maior do que MaxDocumentSize.
var ErrInvalidDocument = errors.New("spolly: o documento não se pode enviar")

// BankAccountParams é a conta bancária dos saques de uma conta ligada.
type BankAccountParams struct {
	// IBAN é o IBAN angolano (AO06...), com ou sem espaços.
	IBAN string `json:"iban"`
	// Holder é o titular, tal como no comprovativo do banco.
	Holder string `json:"holder"`
	// HolderTaxID é o NIF do titular, quando o nome não bate letra a letra
	// com o LegalName da conta.
	HolderTaxID string `json:"holder_tax_id,omitempty"`
	// ProofDocument é um documento DocumentBankAccountProof desta conta
	// (doc_...), já entregue com UploadAccountDocument.
	ProofDocument string `json:"proof_document"`
}

// Balance é o saldo de uma conta, por moeda, lido do livro. Os valores têm
// sinal: um saldo pode ser negativo (um estorno maior do que o disponível).
type Balance struct {
	Object string `json:"object"`
	// Account é a conta do saldo (mer_...).
	Account string `json:"account"`
	// Available é o que se pode sacar.
	Available []Money `json:"available"`
	// Pending ainda não foi libertado.
	Pending []Money `json:"pending"`
	// Reserved é a reserva.
	Reserved []Money `json:"reserved"`
}

func (b *Balance) valido() bool {
	return b.Object == "balance" && strings.HasPrefix(b.Account, "mer_")
}

// Payout é um saque do saldo disponível para a conta bancária verificada.
type Payout struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	Amount Money  `json:"amount"`
	// Status é um dos Payout*.
	Status string `json:"status"`
	// FailureReason só em PayoutFailed: o motivo da recusa, escrito pela
	// equipa.
	FailureReason string `json:"failure_reason,omitempty"`
	// PaidAt é quando a transferência ficou feita, ou nil.
	PaidAt    *time.Time        `json:"paid_at,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func (p *Payout) valido() bool {
	return strings.HasPrefix(p.ID, "po_") && p.Object == "payout"
}

// PayoutParams é o pedido de um saque.
type PayoutParams struct {
	// Amount em unidades menores; em AOA, kwanzas inteiros (múltiplo de 100).
	// Mínimo 5.000 Kz (500000).
	Amount int64 `json:"amount"`
	// Currency é AOA, a única por agora; vazio é AOA.
	Currency Currency `json:"currency,omitempty"`
	// Metadata volta tal como foi, no saque e nos eventos payout.*.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// PayoutList é a lista dos saques de uma conta ligada, os mais recentes
// primeiro.
type PayoutList struct {
	Object  string   `json:"object"`
	Data    []Payout `json:"data"`
	HasMore bool     `json:"has_more"`
}

func (l *PayoutList) valido() bool {
	if l.Object != "list" || l.Data == nil {
		return false
	}
	for i := range l.Data {
		if !l.Data[i].valido() {
			return false
		}
	}
	return true
}

// caminhoDaConta é /v1/accounts/{id}, mais o resto.
func caminhoDaConta(id, resto string) string {
	return caminhoCom("/v1/accounts", id) + resto
}

// CreateAccount cria uma conta ligada (POST /v1/accounts), no modo da chave.
// Com o LegalName, um DocumentIdentity e a conta bancária com o
// comprovativo, a verificação segue sozinha para análise; em teste, aprova-se
// sozinha. Uma Reference repetida é *Error com CodeReferenceRepeated.
func (c *Client) CreateAccount(ctx context.Context, p *AccountParams, opts ...RequestOption) (*ConnectedAccount, error) {
	var a ConnectedAccount
	if err := c.pedir(ctx, http.MethodPost, "/v1/accounts", p, &a, opts); err != nil {
		return nil, err
	}
	return &a, nil
}

// ListAccounts lista as contas ligadas (GET /v1/accounts), da mais recente
// para a mais antiga, no modo da chave.
func (c *Client) ListAccounts(ctx context.Context, p *ListAccountsParams) (*AccountList, error) {
	caminho := "/v1/accounts"
	if p != nil {
		q := url.Values{}
		if p.Limit > 0 {
			q.Set("limit", strconv.Itoa(p.Limit))
		}
		if p.StartingAfter != "" {
			q.Set("starting_after", p.StartingAfter)
		}
		if p.Reference != "" {
			q.Set("reference", p.Reference)
		}
		if len(q) > 0 {
			caminho += "?" + q.Encode()
		}
	}
	var l AccountList
	if err := c.pedir(ctx, http.MethodGet, caminho, nil, &l, nil); err != nil {
		return nil, err
	}
	return &l, nil
}

// GetConnectedAccount lê uma conta ligada (GET /v1/accounts/{id}). Uma conta
// que não é desta plataforma, ou deste modo, é *Error com
// CodeAccountNotFound.
func (c *Client) GetConnectedAccount(ctx context.Context, id string) (*ConnectedAccount, error) {
	var a ConnectedAccount
	if err := c.pedir(ctx, http.MethodGet, caminhoDaConta(id, ""), nil, &a, nil); err != nil {
		return nil, err
	}
	return &a, nil
}

// UpdateAccount altera uma conta ligada (PATCH /v1/accounts/{id}): só muda o
// que vier em p. O LegalName e o TaxID com a verificação em análise ou
// aprovada são *Error com CodeVerificationLocked.
func (c *Client) UpdateAccount(ctx context.Context, id string, p *AccountParams) (*ConnectedAccount, error) {
	if p == nil {
		p = &AccountParams{}
	}
	var a ConnectedAccount
	if err := c.pedir(ctx, http.MethodPatch, caminhoDaConta(id, ""), p, &a, nil); err != nil {
		return nil, err
	}
	return &a, nil
}

// UploadAccountDocument entrega um documento da verificação (POST
// /v1/accounts/{id}/documents, em multipart/form-data, com os campos type e
// file). Com a conta já verificada, só entra o DocumentBankAccountProof, para
// mudar de IBAN. Um documento que não se chega a enviar é ErrInvalidDocument.
func (c *Client) UploadAccountDocument(ctx context.Context, id string, p *AccountDocumentParams, opts ...RequestOption) (*AccountDocument, error) {
	dados, tipo, err := multipartDoDocumento(p)
	if err != nil {
		return nil, err
	}
	var d AccountDocument
	if err := c.enviar(ctx, http.MethodPost, caminhoDaConta(id, "/documents"), dados, tipo, &d, opts); err != nil {
		return nil, err
	}
	return &d, nil
}

// multipartDoDocumento escreve o corpo do documento uma vez, para todas as
// tentativas.
func multipartDoDocumento(p *AccountDocumentParams) ([]byte, string, error) {
	switch {
	case p == nil:
		return nil, "", fmt.Errorf("%w: faltam os parâmetros", ErrInvalidDocument)
	case p.FileName == "":
		return nil, "", fmt.Errorf("%w: falta o nome do ficheiro (FileName)", ErrInvalidDocument)
	case p.Content == nil:
		return nil, "", fmt.Errorf("%w: falta o conteúdo (Content)", ErrInvalidDocument)
	}
	conteudo, err := io.ReadAll(io.LimitReader(p.Content, MaxDocumentSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("%w: ler o conteúdo: %w", ErrInvalidDocument, err)
	}
	if len(conteudo) > MaxDocumentSize {
		return nil, "", fmt.Errorf("%w: o ficheiro passa os %d bytes", ErrInvalidDocument, MaxDocumentSize)
	}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	// Escrever para um bytes.Buffer não falha.
	_ = w.WriteField("type", p.Type)
	parte, _ := w.CreateFormFile("file", p.FileName)
	_, _ = parte.Write(conteudo)
	_ = w.Close()
	return b.Bytes(), w.FormDataContentType(), nil
}

// SetAccountBankAccount define a conta bancária dos saques (PUT
// /v1/accounts/{id}/bank_account), com o comprovativo já entregue. Com a conta
// verificada, o IBAN novo fica BankAccountPendingVerification até a equipa o
// verificar: até lá não há saques, e nas 72 horas seguintes nenhum saque se
// aprova sozinho.
func (c *Client) SetAccountBankAccount(ctx context.Context, id string, p *BankAccountParams) (*ConnectedAccount, error) {
	var a ConnectedAccount
	if err := c.pedir(ctx, http.MethodPut, caminhoDaConta(id, "/bank_account"), p, &a, nil); err != nil {
		return nil, err
	}
	return &a, nil
}

// GetAccountBalance lê o saldo de uma conta ligada (GET
// /v1/accounts/{id}/balance), do livro, por moeda.
func (c *Client) GetAccountBalance(ctx context.Context, id string) (*Balance, error) {
	var b Balance
	if err := c.pedir(ctx, http.MethodGet, caminhoDaConta(id, "/balance"), nil, &b, nil); err != nil {
		return nil, err
	}
	return &b, nil
}

// CreateAccountPayout pede um saque do saldo disponível de uma conta ligada
// para a conta bancária verificada (POST /v1/accounts/{id}/payouts). Até ao
// limite da habilitação, sem nada que o risco aponte e sem um IBAN mudado nas
// últimas 72 horas, aprova-se sozinho (PayoutProcessing); os outros ficam
// PayoutPending, à espera da equipa. Em teste, aprova-se e paga-se sozinho.
// Nunca mais do que o disponível: acima disso, *Error com
// CodeInsufficientBalance.
func (c *Client) CreateAccountPayout(ctx context.Context, id string, p *PayoutParams, opts ...RequestOption) (*Payout, error) {
	var s Payout
	if err := c.pedir(ctx, http.MethodPost, caminhoDaConta(id, "/payouts"), p, &s, opts); err != nil {
		return nil, err
	}
	return &s, nil
}

// ListAccountPayouts lista os 100 saques mais recentes de uma conta ligada
// (GET /v1/accounts/{id}/payouts).
func (c *Client) ListAccountPayouts(ctx context.Context, id string) (*PayoutList, error) {
	var l PayoutList
	if err := c.pedir(ctx, http.MethodGet, caminhoDaConta(id, "/payouts"), nil, &l, nil); err != nil {
		return nil, err
	}
	return &l, nil
}

// GetAccountPayout lê um saque de uma conta ligada (GET
// /v1/accounts/{id}/payouts/{payout_id}).
func (c *Client) GetAccountPayout(ctx context.Context, id, payoutID string) (*Payout, error) {
	var s Payout
	if err := c.pedir(ctx, http.MethodGet, caminhoCom(caminhoDaConta(id, "/payouts"), payoutID), nil, &s, nil); err != nil {
		return nil, err
	}
	return &s, nil
}
