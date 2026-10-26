package spolly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const contaJSON = `{"id":"mer_01JA9VENDEDOR000000","object":"account","mode":"test","name":"Kizomba Eventos","legal_name":"Joana Manuel",
"tax_id":"","email":"joana@kizomba.ao","phone":"+244923456789","reference":"vendedor_481","metadata":{"plano":"pro"},
"verification":{"status":"pending","reason":"","missing":["identity_document","bank_account"]},
"bank_account":null,"documents":[],"charges_enabled":true,"payouts_enabled":false,"created_at":"2026-11-29T08:55:00Z"}`

const contaVerificadaJSON = `{"id":"mer_01JA9VENDEDOR000000","object":"account","mode":"test","name":"Kizomba Eventos","legal_name":"Joana Manuel",
"tax_id":"","email":"","phone":"","reference":"vendedor_481","metadata":{},
"verification":{"status":"verified","reason":"","missing":[]},
"bank_account":{"id":"cb_01JA9CONTA","iban_last4":"9012","holder":"Joana Manuel","status":"pending_verification","created_at":"2026-11-29T09:00:00Z"},
"documents":[{"id":"doc_01JA9BI","object":"account_document","type":"identity_document","file_name":"bi.pdf","content_type":"application/pdf","size":182340,"created_at":"2026-11-29T08:58:00Z"}],
"charges_enabled":true,"payouts_enabled":false,"created_at":"2026-11-29T08:55:00Z"}`

const documentoJSON = `{"id":"doc_01JA9IBAN","object":"account_document","type":"bank_account_proof","file_name":"iban.pdf",
"content_type":"application/pdf","size":9,"created_at":"2026-11-29T08:59:00Z"}`

const saldoJSON = `{"object":"balance","account":"mer_01JA9VENDEDOR000000","available":[{"amount":-150000,"currency":"AOA"}],
"pending":[{"amount":4875000,"currency":"AOA"}],"reserved":[]}`

const saqueJSON = `{"id":"po_01JA9P","object":"payout","amount":{"amount":2500000,"currency":"AOA"},"status":"paid",
"paid_at":"2026-11-30T11:00:00Z","created_at":"2026-11-30T10:15:00Z","metadata":{"pedido_na_plataforma":"saq_9921"}}`

// cabecalhosDaConta grava o Spolly-Account de cada pedido, pela ordem.
type cabecalhosDaConta struct {
	mu    sync.Mutex
	lista []string
}

func (c *cabecalhosDaConta) gravar(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lista = append(c.lista, r.Header.Get(AccountHeader))
}

func respostasDasContas(contas *cabecalhosDaConta) func(int, *http.Request) (int, http.Header, string) {
	return func(_ int, r *http.Request) (int, http.Header, string) {
		contas.gravar(r)
		p := r.URL.Path
		switch {
		case r.Method == "GET" && p == "/v1/accounts":
			return 200, nil, `{"object":"list","data":[` + contaJSON + `],"has_more":true}`
		case strings.HasSuffix(p, "/documents"):
			return 201, nil, documentoJSON
		case strings.HasSuffix(p, "/bank_account"):
			return 200, nil, contaVerificadaJSON
		case strings.HasSuffix(p, "/balance"):
			return 200, nil, saldoJSON
		case r.Method == "GET" && strings.HasSuffix(p, "/payouts"):
			return 200, nil, `{"object":"list","data":[` + saqueJSON + `],"has_more":false}`
		case strings.Contains(p, "/payouts"):
			return 201, nil, saqueJSON
		case p == "/v1/conta":
			return 200, nil, `{"merchant_id":"mer_01JA9VENDEDOR000000","nome_comercial":"Kizomba Eventos","modo":"test"}`
		case strings.HasPrefix(p, "/v1/refunds"):
			return 201, nil, estornoJSON
		case strings.HasPrefix(p, "/v1/subscriptions"):
			return 200, nil, subscricaoJSON
		case strings.HasPrefix(p, "/v1/checkout"):
			return 201, nil, sessaoJSON
		}
		return 200, nil, contaJSON
	}
}

func TestAsContasLigadas(t *testing.T) {
	contas := &cabecalhosDaConta{}
	c, s, _ := novoCliente(t, respostasDasContas(contas))
	ctx := context.Background()
	const id = "mer_01JA9VENDEDOR000000"

	a, err := c.CreateAccount(ctx, &AccountParams{Name: "Kizomba Eventos", LegalName: "Joana Manuel", Email: "joana@kizomba.ao",
		Phone: "+244923456789", Reference: "vendedor_481", Metadata: map[string]string{"plano": "pro"}})
	if err != nil || a.ID != id || a.Mode != ModeTest || a.Verification.Status != VerificationPending || a.BankAccount != nil ||
		len(a.Verification.Missing) != 2 || a.Verification.Missing[0] != MissingIdentityDocument || !a.ChargesEnabled || a.PayoutsEnabled {
		t.Fatalf("%+v %v", a, err)
	}
	l, err := c.ListAccounts(ctx, &ListAccountsParams{Limit: 50, StartingAfter: "mer_01JA9ANTERIOR00000", Reference: "vendedor 481"})
	if err != nil || !l.HasMore || len(l.Data) != 1 || l.Data[0].Reference != "vendedor_481" {
		t.Fatalf("%+v %v", l, err)
	}
	if _, err := c.ListAccounts(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListAccounts(ctx, &ListAccountsParams{}); err != nil {
		t.Fatal(err)
	}
	if a, err = c.GetConnectedAccount(ctx, id); err != nil || a.Name != "Kizomba Eventos" {
		t.Fatal(a, err)
	}
	if _, err := c.UpdateAccount(ctx, id, &AccountParams{TaxID: "5417123456"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateAccount(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	d, err := c.UploadAccountDocument(ctx, id, &AccountDocumentParams{Type: DocumentBankAccountProof, FileName: "iban.pdf",
		Content: strings.NewReader("%PDF-1.7\n")}, WithIdempotencyKey("doc-iban-1"))
	if err != nil || d.ID != "doc_01JA9IBAN" || d.Type != DocumentBankAccountProof || d.Size != 9 {
		t.Fatal(d, err)
	}
	a, err = c.SetAccountBankAccount(ctx, id, &BankAccountParams{IBAN: "AO06004000010123456789012", Holder: "Joana Manuel", ProofDocument: d.ID})
	if err != nil || a.BankAccount.Status != BankAccountPendingVerification || a.BankAccount.IBANLast4 != "9012" ||
		a.Verification.Status != VerificationVerified || len(a.Documents) != 1 || a.Documents[0].Type != DocumentIdentity {
		t.Fatalf("%+v %v", a, err)
	}
	b, err := c.GetAccountBalance(ctx, id)
	if err != nil || b.Available[0] != (Money{-150000, AOA}) || b.Pending[0].Amount != 4875000 || len(b.Reserved) != 0 {
		t.Fatalf("%+v %v", b, err)
	}
	po, err := c.CreateAccountPayout(ctx, id, &PayoutParams{Amount: 2500000, Currency: AOA, Metadata: map[string]string{"pedido_na_plataforma": "saq_9921"}})
	if err != nil || po.Status != PayoutPaid || po.PaidAt == nil || po.Amount.Amount != 2500000 || po.Metadata["pedido_na_plataforma"] != "saq_9921" {
		t.Fatalf("%+v %v", po, err)
	}
	ps, err := c.ListAccountPayouts(ctx, id)
	if err != nil || len(ps.Data) != 1 || ps.HasMore {
		t.Fatalf("%+v %v", ps, err)
	}
	if po, err = c.GetAccountPayout(ctx, id, "po_01JA9P"); err != nil || po.ID != "po_01JA9P" {
		t.Fatal(po, err)
	}

	p := s.pedidos
	esperado := []struct{ metodo, caminho, corpo string }{
		{"POST", "/v1/accounts", `{"name":"Kizomba Eventos","legal_name":"Joana Manuel","email":"joana@kizomba.ao","phone":"+244923456789","reference":"vendedor_481","metadata":{"plano":"pro"}}`},
		{"GET", "/v1/accounts", ""},
		{"GET", "/v1/accounts", ""},
		{"GET", "/v1/accounts", ""},
		{"GET", "/v1/accounts/" + id, ""},
		{"PATCH", "/v1/accounts/" + id, `{"tax_id":"5417123456"}`},
		{"PATCH", "/v1/accounts/" + id, `{}`},
		{"POST", "/v1/accounts/" + id + "/documents", ""},
		{"PUT", "/v1/accounts/" + id + "/bank_account", `{"iban":"AO06004000010123456789012","holder":"Joana Manuel","proof_document":"doc_01JA9IBAN"}`},
		{"GET", "/v1/accounts/" + id + "/balance", ""},
		{"POST", "/v1/accounts/" + id + "/payouts", `{"amount":2500000,"currency":"AOA","metadata":{"pedido_na_plataforma":"saq_9921"}}`},
		{"GET", "/v1/accounts/" + id + "/payouts", ""},
		{"GET", "/v1/accounts/" + id + "/payouts/po_01JA9P", ""},
	}
	if len(p) != len(esperado) {
		t.Fatal(len(p))
	}
	for i, e := range esperado {
		if p[i].Metodo != e.metodo || p[i].Caminho != e.caminho || (e.corpo != "" && p[i].Corpo != e.corpo) {
			t.Errorf("%d: %+v, esperado %+v", i, p[i], e)
		}
		// Só os POST levam a Idempotency-Key.
		if (e.metodo == "POST") != (p[i].Chave != "") {
			t.Errorf("%d: chave %q", i, p[i].Chave)
		}
		// As rotas das contas nunca levam o Spolly-Account.
		if contas.lista[i] != "" {
			t.Errorf("%d: Spolly-Account %q", i, contas.lista[i])
		}
	}
	if !uuid4.MatchString(p[0].Chave) || !uuid4.MatchString(p[10].Chave) || p[7].Chave != "doc-iban-1" {
		t.Fatal(p[0].Chave, p[7].Chave, p[10].Chave)
	}
	if p[5].Tipo != "application/json" || p[1].Tipo != "" {
		t.Fatal(p[5].Tipo, p[1].Tipo)
	}

	// O documento vai em multipart, com o type e o file.
	tipo, params, err := mime.ParseMediaType(p[7].Tipo)
	if err != nil || tipo != "multipart/form-data" {
		t.Fatal(p[7].Tipo, err)
	}
	form, err := multipart.NewReader(strings.NewReader(p[7].Corpo), params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	if form.Value["type"][0] != DocumentBankAccountProof || form.File["file"][0].Filename != "iban.pdf" {
		t.Fatalf("%+v", form)
	}
	f, _ := form.File["file"][0].Open()
	conteudo, _ := io.ReadAll(f)
	if string(conteudo) != "%PDF-1.7\n" {
		t.Fatalf("%q", conteudo)
	}
}

func TestListarContasLevaOsFiltrosNaQuery(t *testing.T) {
	var query []string
	c, _, _ := novoCliente(t, func(_ int, r *http.Request) (int, http.Header, string) {
		query = append(query, r.URL.RawQuery)
		return 200, nil, `{"object":"list","data":[],"has_more":false}`
	})
	ctx := context.Background()
	if l, err := c.ListAccounts(ctx, &ListAccountsParams{Limit: 50, StartingAfter: "mer_01JA9ANTERIOR00000", Reference: "vendedor 481"}); err != nil || len(l.Data) != 0 {
		t.Fatal(l, err)
	}
	if _, err := c.ListAccounts(ctx, &ListAccountsParams{Reference: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListAccounts(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if query[0] != "limit=50&reference=vendedor+481&starting_after=mer_01JA9ANTERIOR00000" || query[1] != "reference=x" || query[2] != "" {
		t.Fatal(query)
	}
}

func TestEmNomeDeUmaContaLigada(t *testing.T) {
	contas := &cabecalhosDaConta{}
	plataforma, s, _ := novoCliente(t, respostasDasContas(contas))
	vendedor := plataforma.WithAccount("mer_01JA9VENDEDOR000000")
	ctx := context.Background()

	if _, err := vendedor.CreateCheckoutSession(ctx, &CheckoutSessionParams{Amount: 1500000, Currency: AOA, Description: "Bilhete",
		PlatformFee: &PlatformFee{Amount: 150000}}); err != nil {
		t.Fatal(err)
	}
	if _, err := vendedor.GetCheckoutSession(ctx, "cs_test_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := vendedor.CreateRefund(ctx, &RefundParams{PaymentID: "pay_1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := vendedor.GetRefund(ctx, "re_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := vendedor.GetSubscription(ctx, "sub_1"); err != nil {
		t.Fatal(err)
	}
	if a, err := vendedor.GetAccount(ctx); err != nil || a.MerchantID != "mer_01JA9VENDEDOR000000" {
		t.Fatal(a, err)
	}
	// As rotas das contas, mesmo pela cópia, não levam o cabeçalho.
	if _, err := vendedor.GetAccountBalance(ctx, "mer_01JA9VENDEDOR000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := vendedor.ListAccounts(ctx, &ListAccountsParams{Limit: 1}); err != nil {
		t.Fatal(err)
	}
	// O original continua a pedir em nome da própria chave, e um id vazio
	// também.
	if _, err := plataforma.GetCheckoutSession(ctx, "cs_test_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := vendedor.WithAccount("").GetCheckoutSession(ctx, "cs_test_1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"mer_01JA9VENDEDOR000000", "mer_01JA9VENDEDOR000000", "mer_01JA9VENDEDOR000000", "mer_01JA9VENDEDOR000000",
		"mer_01JA9VENDEDOR000000", "mer_01JA9VENDEDOR000000", "", "", "", ""}
	if strings.Join(contas.lista, ",") != strings.Join(want, ",") {
		t.Fatalf("\n%v\n%v", contas.lista, want)
	}
	if s.pedidos[0].Corpo != `{"amount":1500000,"currency":"AOA","description":"Bilhete","platform_fee":{"amount":150000}}` {
		t.Fatal(s.pedidos[0].Corpo)
	}
}

func TestDaPlataforma(t *testing.T) {
	for caminho, want := range map[string]bool{
		"/v1/accounts": true, "/v1/accounts?limit=1": true, "/v1/accounts/mer_1/payouts": true,
		"/v1/accountsx": false, "/v1/conta": false, "/v1/refunds": false,
	} {
		if daPlataforma(caminho) != want {
			t.Errorf("%s: %v", caminho, !want)
		}
	}
}

type leitorQueFalha struct{}

func (leitorQueFalha) Read([]byte) (int, error) { return 0, errors.New("o disco falhou") }

func TestUmDocumentoQueNaoSeChegaAEnviar(t *testing.T) {
	c, s, _ := novoCliente(t, sempre(201, documentoJSON))
	ctx := context.Background()
	for nome, p := range map[string]*AccountDocumentParams{
		"sem parâmetros": nil,
		"sem nome":       {Type: DocumentIdentity, Content: strings.NewReader("x")},
		"sem conteúdo":   {Type: DocumentIdentity, FileName: "bi.pdf"},
		"leitura falha":  {Type: DocumentIdentity, FileName: "bi.pdf", Content: leitorQueFalha{}},
		"grande de mais": {Type: DocumentIdentity, FileName: "bi.pdf", Content: bytes.NewReader(make([]byte, MaxDocumentSize+1))},
	} {
		d, err := c.UploadAccountDocument(ctx, "mer_1", p)
		if d != nil || !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("%s: %v %v", nome, d, err)
		}
	}
	if len(s.pedidos) != 0 {
		t.Fatal(len(s.pedidos))
	}
	// Exatamente o máximo ainda segue.
	if _, err := c.UploadAccountDocument(ctx, "mer_1", &AccountDocumentParams{Type: DocumentIdentity, FileName: "bi.pdf",
		Content: bytes.NewReader(make([]byte, MaxDocumentSize))}); err != nil {
		t.Fatal(err)
	}
}

func TestAsRecusasDasContasLigadas(t *testing.T) {
	ctx := context.Background()
	c, _, _ := novoCliente(t, sempre(403, `{"erro":"contas_ligadas_nao_habilitadas","mensagem":"Esta conta não tem as contas ligadas habilitadas."}`))
	chamadas := []func() error{
		func() error { _, err := c.CreateAccount(ctx, &AccountParams{Name: "x"}); return err },
		func() error { _, err := c.ListAccounts(ctx, nil); return err },
		func() error { _, err := c.GetConnectedAccount(ctx, "mer_1"); return err },
		func() error { _, err := c.UpdateAccount(ctx, "mer_1", nil); return err },
		func() error {
			_, err := c.UploadAccountDocument(ctx, "mer_1", &AccountDocumentParams{Type: DocumentIdentity, FileName: "bi.pdf", Content: strings.NewReader("x")})
			return err
		},
		func() error { _, err := c.SetAccountBankAccount(ctx, "mer_1", &BankAccountParams{}); return err },
		func() error { _, err := c.GetAccountBalance(ctx, "mer_1"); return err },
		func() error { _, err := c.CreateAccountPayout(ctx, "mer_1", &PayoutParams{Amount: 500000}); return err },
		func() error { _, err := c.ListAccountPayouts(ctx, "mer_1"); return err },
		func() error { _, err := c.GetAccountPayout(ctx, "mer_1", "po_1"); return err },
	}
	for i, f := range chamadas {
		if err := f(); ErrorCode(err) != CodeConnectedAccountsNotEnabled {
			t.Errorf("%d: %v", i, err)
		}
	}
	c, _, _ = novoCliente(t, sempre(409, `{"erro":"saldo_insuficiente","mensagem":"O saque passa o saldo disponível."}`))
	if _, err := c.CreateAccountPayout(ctx, "mer_1", &PayoutParams{Amount: 90000000}); !errors.Is(err, &Error{Code: CodeInsufficientBalance}) {
		t.Fatal(err)
	}
}

func TestUmaRespostaDasContasQueNaoSeLe(t *testing.T) {
	ctx := context.Background()
	for _, corpo := range []string{`{}`, `não é json`, `{"object":"list"}`, `{"object":"list","data":[{"id":"x"}],"has_more":false}`,
		`{"object":"balance","account":"x"}`, `{"id":"po_1","object":"refund"}`} {
		c, _, _ := novoCliente(t, sempre(200, corpo))
		if a, err := c.GetConnectedAccount(ctx, "mer_1"); a != nil || !errors.Is(err, ErrInvalidResponse) {
			t.Fatal(corpo, a, err)
		}
		if l, err := c.ListAccounts(ctx, nil); l != nil || !errors.Is(err, ErrInvalidResponse) {
			t.Fatal(corpo, l, err)
		}
		if d, err := c.UploadAccountDocument(ctx, "mer_1", &AccountDocumentParams{FileName: "a.pdf", Content: strings.NewReader("x")}); d != nil || !errors.Is(err, ErrInvalidResponse) {
			t.Fatal(corpo, d, err)
		}
		if b, err := c.GetAccountBalance(ctx, "mer_1"); b != nil || !errors.Is(err, ErrInvalidResponse) {
			t.Fatal(corpo, b, err)
		}
		if p, err := c.GetAccountPayout(ctx, "mer_1", "po_1"); p != nil || !errors.Is(err, ErrInvalidResponse) {
			t.Fatal(corpo, p, err)
		}
		if l, err := c.ListAccountPayouts(ctx, "mer_1"); l != nil || !errors.Is(err, ErrInvalidResponse) {
			t.Fatal(corpo, l, err)
		}
	}
	c, _, _ := novoCliente(t, sempre(200, `{"object":"list","data":[{"id":"po_1","object":"payout"},{"id":"re_1","object":"refund"}],"has_more":false}`))
	if l, err := c.ListAccountPayouts(ctx, "mer_1"); l != nil || !errors.Is(err, ErrInvalidResponse) {
		t.Fatal(l, err)
	}
}

// As contas, o saldo e o saque do contrato (contratos/eventos/objetos.schema.json)
// lidos para os tipos e escritos de volta dão os mesmos campos.
func TestOsTiposDasContasBatemComOContrato(t *testing.T) {
	for _, k := range []struct {
		corpo string
		dst   any
	}{{contaVerificadaJSON, &ConnectedAccount{}}, {documentoJSON, &AccountDocument{}}, {saldoJSON, &Balance{}}, {saqueJSON, &Payout{}}} {
		camposIguais(t, k.corpo, k.dst)
	}
}

func camposIguais(t *testing.T, corpo string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(corpo), dst); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(dst)
	var a, d map[string]any
	_ = json.Unmarshal([]byte(corpo), &a)
	_ = json.Unmarshal(b, &d)
	for campo := range a {
		if _, ok := d[campo]; !ok {
			t.Errorf("o campo %q não está no tipo %T", campo, dst)
		}
	}
}
