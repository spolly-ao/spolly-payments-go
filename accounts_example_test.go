package spolly_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	spolly "github.com/spolly-ao/spolly-payments-go"
)

// apiDasContas faz de API do Spolly Payments para os exemplos das contas
// ligadas, com os corpos do contrato. Como a API, recusa o Spolly-Account nas
// rotas /v1/accounts.
func apiDasContas() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		corpo, _ := io.ReadAll(r.Body)
		conta := r.Header.Get(spolly.AccountHeader)
		if conta != "" && strings.HasPrefix(r.URL.Path, "/v1/accounts") {
			w.WriteHeader(400)
			io.WriteString(w, `{"erro":"spolly_account_nao_serve_aqui","mensagem":"As rotas /v1/accounts são da plataforma."}`)
			return
		}
		const id = "/v1/accounts/mer_01JA9VENDEDOR000000"
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/accounts":
			w.WriteHeader(201)
			io.WriteString(w, contaLigada(`"pending"`, `null`))
			return
		case "GET /v1/accounts":
			io.WriteString(w, `{"object":"list","data":[`+contaLigada(`"verified"`, contaBancaria)+`],"has_more":false}`)
			return
		case "GET " + id:
			io.WriteString(w, contaLigada(`"verified"`, contaBancaria))
			return
		case "PATCH " + id:
			io.WriteString(w, strings.Replace(contaLigada(`"pending"`, `null`), `"Kizomba Eventos"`, `"Kizomba Eventos Luanda"`, 1))
			return
		case "POST " + id + "/documents":
			tipo := "identity_document"
			if strings.Contains(string(corpo), "bank_account_proof") {
				tipo = "bank_account_proof"
			}
			w.WriteHeader(201)
			io.WriteString(w, `{"id":"doc_01JA9IBANxxxxxxxxxxxxx","object":"account_document","type":"`+tipo+`","file_name":"iban.pdf",
"content_type":"application/pdf","size":90211,"created_at":"2026-11-29T08:59:00Z"}`)
			return
		case "PUT " + id + "/bank_account":
			io.WriteString(w, contaLigada(`"verified"`, strings.Replace(contaBancaria, `"verified"`, `"pending_verification"`, 1)))
			return
		case "GET " + id + "/balance":
			io.WriteString(w, `{"object":"balance","account":"mer_01JA9VENDEDOR000000","available":[{"amount":4875000,"currency":"AOA"}],
"pending":[{"amount":1500000,"currency":"AOA"}],"reserved":[{"amount":0,"currency":"AOA"}]}`)
			return
		case "POST " + id + "/payouts":
			w.WriteHeader(201)
			io.WriteString(w, saque("processing"))
			return
		case "GET " + id + "/payouts":
			io.WriteString(w, `{"object":"list","data":[`+saque("paid")+`],"has_more":false}`)
			return
		case "GET " + id + "/payouts/po_01JA9Pxxxxxxxxxxxxxxx":
			io.WriteString(w, saque("paid"))
			return
		case "POST /v1/checkout/sessions":
			if conta == "mer_01JA9VENDEDOR000000" {
				w.WriteHeader(201)
				io.WriteString(w, sessao("open", `null`, `null`))
				return
			}
		}
		w.WriteHeader(404)
		io.WriteString(w, `{"erro":"conta_nao_existe","mensagem":"Esta conta ligada não existe nesta plataforma, neste modo."}`)
	}))
}

const contaBancaria = `{"id":"cb_01JA9CONTA","iban_last4":"9012","holder":"Joana Manuel","status":"verified","created_at":"2026-11-29T09:00:00Z"}`

func contaLigada(verificacao, banco string) string {
	falta := `["identity_document","bank_account"]`
	if verificacao != `"pending"` {
		falta = `[]`
	}
	return `{"id":"mer_01JA9VENDEDOR000000","object":"account","mode":"test","name":"Kizomba Eventos","legal_name":"Joana Manuel",
"tax_id":"","email":"joana@kizomba.ao","phone":"+244923456789","reference":"vendedor_481","metadata":{"plano":"pro"},
"verification":{"status":` + verificacao + `,"reason":"","missing":` + falta + `},"bank_account":` + banco + `,"documents":[],
"charges_enabled":true,"payouts_enabled":` + fmt.Sprint(banco != `null`) + `,"created_at":"2026-11-29T08:55:00Z"}`
}

func saque(estado string) string {
	return `{"id":"po_01JA9Pxxxxxxxxxxxxxxx","object":"payout","amount":{"amount":2500000,"currency":"AOA"},"status":"` + estado + `",
"created_at":"2026-11-30T10:15:00Z","metadata":{"pedido_na_plataforma":"saq_9921"}}`
}

// Uma plataforma cria o vendedor. A Reference é o id dele do lado da
// plataforma, e fica única por plataforma e modo.
func ExampleClient_CreateAccount() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	conta, err := plataforma.CreateAccount(context.Background(), &spolly.AccountParams{
		Name:      "Kizomba Eventos",
		LegalName: "Joana Manuel",
		Email:     "joana@kizomba.ao",
		Phone:     "+244923456789",
		Reference: "vendedor_481",
		Metadata:  map[string]string{"plano": "pro"},
	}, spolly.WithIdempotencyKey("vendedor_481"))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(conta.ID, conta.Verification.Status, conta.Verification.Missing)
	// Output:
	// mer_01JA9VENDEDOR000000 pending [identity_document bank_account]
}

// Encontrar a conta de um vendedor pela referência da plataforma.
func ExampleClient_ListAccounts() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	pagina, err := plataforma.ListAccounts(context.Background(), &spolly.ListAccountsParams{Reference: "vendedor_481"})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, conta := range pagina.Data {
		fmt.Println(conta.ID, conta.Name, conta.PayoutsEnabled)
	}
	// Output:
	// mer_01JA9VENDEDOR000000 Kizomba Eventos true
}

// Ler uma conta ligada, por exemplo para saber se já pode sacar.
func ExampleClient_GetConnectedAccount() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	conta, err := plataforma.GetConnectedAccount(context.Background(), "mer_01JA9VENDEDOR000000")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(conta.Verification.Status, conta.BankAccount.IBANLast4, conta.PayoutsEnabled)
	// Output:
	// verified 9012 true
}

// Mudar o nome que o comprador vê; o resto fica como estava.
func ExampleClient_UpdateAccount() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	conta, err := plataforma.UpdateAccount(context.Background(), "mer_01JA9VENDEDOR000000", &spolly.AccountParams{Name: "Kizomba Eventos Luanda"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(conta.Name)
	// Output:
	// Kizomba Eventos Luanda
}

// Entregar o comprovativo do IBAN. Em produção, o conteúdo vem de um ficheiro
// (os.Open) ou do upload do vendedor na plataforma.
func ExampleClient_UploadAccountDocument() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	doc, err := plataforma.UploadAccountDocument(context.Background(), "mer_01JA9VENDEDOR000000", &spolly.AccountDocumentParams{
		Type:     spolly.DocumentBankAccountProof,
		FileName: "iban.pdf",
		Content:  strings.NewReader("%PDF-1.7 ..."),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(doc.ID, doc.Type, doc.ContentType)
	// Output:
	// doc_01JA9IBANxxxxxxxxxxxxx bank_account_proof application/pdf
}

// Definir a conta bancária dos saques, com o comprovativo já entregue. Numa
// conta verificada, o IBAN novo espera pela equipa.
func ExampleClient_SetAccountBankAccount() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	conta, err := plataforma.SetAccountBankAccount(context.Background(), "mer_01JA9VENDEDOR000000", &spolly.BankAccountParams{
		IBAN:          "AO06004000010123456789012",
		Holder:        "Joana Manuel",
		ProofDocument: "doc_01JA9IBANxxxxxxxxxxxxx",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(conta.BankAccount.Status == spolly.BankAccountPendingVerification)
	// Output:
	// true
}

// O saldo de um vendedor, do livro. Os valores têm sinal: um saldo pode ser
// negativo.
func ExampleClient_GetAccountBalance() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	saldo, err := plataforma.GetAccountBalance(context.Background(), "mer_01JA9VENDEDOR000000")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, m := range saldo.Available {
		fmt.Println("disponível:", m.Amount, m.Currency)
	}
	// Output:
	// disponível: 4875000 AOA
}

// Pedir um saque para a conta bancária verificada do vendedor. A
// Idempotency-Key própria (o pedido de saque na plataforma) faz com que
// repetir não saque duas vezes.
func ExampleClient_CreateAccountPayout() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	saque, err := plataforma.CreateAccountPayout(context.Background(), "mer_01JA9VENDEDOR000000", &spolly.PayoutParams{
		Amount:   2500000, // 25.000,00 Kz
		Currency: spolly.AOA,
		Metadata: map[string]string{"pedido_na_plataforma": "saq_9921"},
	}, spolly.WithIdempotencyKey("saq_9921"))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(saque.ID, saque.Status)
	// Output:
	// po_01JA9Pxxxxxxxxxxxxxxx processing
}

// Os saques de um vendedor, os mais recentes primeiro.
func ExampleClient_ListAccountPayouts() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	saques, err := plataforma.ListAccountPayouts(context.Background(), "mer_01JA9VENDEDOR000000")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, s := range saques.Data {
		fmt.Println(s.ID, s.Status, s.Amount.Amount)
	}
	// Output:
	// po_01JA9Pxxxxxxxxxxxxxxx paid 2500000
}

// O desfecho de um saque.
func ExampleClient_GetAccountPayout() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	saque, err := plataforma.GetAccountPayout(context.Background(), "mer_01JA9VENDEDOR000000", "po_01JA9Pxxxxxxxxxxxxxxx")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(saque.Status == spolly.PayoutPaid)
	// Output:
	// true
}

// Cobrar em nome de um vendedor: a cópia de WithAccount manda o
// Spolly-Account em cada pedido, e a sessão é do vendedor. Com PlatformFee e
// Platform vazio, a comissão é da plataforma.
func ExampleClient_WithAccount() {
	api := apiDasContas()
	defer api.Close()

	plataforma := spolly.NewClient("sk_test_exemplo", spolly.WithBaseURL(api.URL))
	vendedor := plataforma.WithAccount("mer_01JA9VENDEDOR000000")
	sessao, err := vendedor.CreateCheckoutSession(context.Background(), &spolly.CheckoutSessionParams{
		Amount:      1500000,
		Currency:    spolly.AOA,
		Description: "Bilhete: Noite de Kizomba",
		PlatformFee: &spolly.PlatformFee{Amount: 150000},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(sessao.Status)

	// As rotas das contas continuam a funcionar pela cópia: não levam o
	// cabeçalho.
	saldo, err := vendedor.GetAccountBalance(context.Background(), "mer_01JA9VENDEDOR000000")
	fmt.Println(saldo.Account, err)
	// Output:
	// open
	// mer_01JA9VENDEDOR000000 <nil>
}
