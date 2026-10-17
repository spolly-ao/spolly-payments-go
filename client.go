// Package spolly é o cliente em Go da API do Spolly Payments: criar e ler
// sessões de checkout, estornar, e confirmar a conta da chave. A verificação
// da assinatura dos webhooks está no subpacote webhook.
//
//	cliente := spolly.NewClient(os.Getenv("SPOLLY_SECRET_KEY"))
//	sessao, err := cliente.CreateCheckoutSession(ctx, &spolly.CheckoutSessionParams{
//		Amount:      1500000, // 15.000,00 Kz, em unidades menores
//		Currency:    spolly.AOA,
//		Description: "Vestido de capulana",
//		SuccessURL:  "https://kiala.ao/obrigado?pedido=1042",
//	})
//	// Mandar o comprador para sessao.URL.
//
// # O que o cliente faz sozinho
//
//   - Idempotência: todos os POST levam o cabeçalho Idempotency-Key. Sem
//     WithIdempotencyKey, o cliente gera um (UUID v4) por chamada, e usa o
//     mesmo em todas as retentativas dessa chamada: uma rede que cai depois de
//     a API gravar não cria a mesma sessão duas vezes. Para ser idempotente
//     entre chamadas (o mesmo pedido da loja, repetido depois de o processo
//     reiniciar), passa-se uma chave própria, por exemplo o número do pedido.
//   - Retentativas: com falha de rede, 429, 500, 502, 503 ou 504, volta a
//     tentar até MaxRetries vezes (2 por omissão), com espera exponencial e
//     aleatória, e respeita o Retry-After. Uma resposta 4xx nunca se repete.
//   - Tempo limite: cada tentativa tem 30 segundos por omissão (WithTimeout),
//     e o ctx de quem chama manda sobre tudo.
//
// # Erros
//
// Uma recusa da API é um *Error, com o código estável (Code, um dos Code*) e
// a mensagem em português; lê-se com errors.As. Uma resposta que não se
// consegue ler nunca vira um valor vazio: é um erro.
//
// # Dinheiro
//
// Sempre em unidades menores inteiras (int64): 15.000,00 Kz é 1500000. Em AOA
// o valor é em kwanzas inteiros (múltiplo de 100).
package spolly

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Version é a versão deste SDK, que vai no User-Agent.
const Version = "0.2.0"

// DefaultBaseURL é a API de produção. As chaves sk_test_ também a usam: o modo
// é o da chave, e não o do endereço.
const DefaultBaseURL = "https://payments-api.spolly.ao"

// Os valores por omissão do cliente.
const (
	DefaultTimeout    = 30 * time.Second
	DefaultMaxRetries = 2
)

// IdempotencyKeyHeader e IdempotentReplayedHeader são os cabeçalhos da
// idempotência.
const (
	IdempotencyKeyHeader     = "Idempotency-Key"
	IdempotentReplayedHeader = "Idempotent-Replayed"
)

// maxResposta é o maior corpo de resposta que se lê. As respostas da API
// ficam muito abaixo disto.
const maxResposta = 4 << 20

// As esperas entre tentativas.
const (
	esperaBase   = 500 * time.Millisecond
	esperaMaxima = 8 * time.Second
	// retryAfterMaximo: um Retry-After acima disto não se espera inteiro.
	retryAfterMaximo = 30 * time.Second
)

// Client fala com a API do Spolly Payments. É seguro usá-lo a partir de
// várias goroutines; cria-se um e reutiliza-se.
type Client struct {
	key        string
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
	maxRetries int
	userAgent  string
	// dormir e aleatorio existem para os testes não esperarem de verdade.
	dormir    func(context.Context, time.Duration) error
	aleatorio func(int64) int64
}

// Option configura o cliente em NewClient.
type Option func(*Client)

// WithBaseURL muda o endereço da API, por exemplo para http://localhost:8084
// em desenvolvimento.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient usa outro *http.Client (um transporte próprio, um proxy).
// O tempo limite de cada tentativa continua a ser o de WithTimeout.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

// WithTimeout é o tempo limite de cada tentativa. Zero, ou negativo, é
// DefaultTimeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithMaxRetries é quantas vezes, no máximo, se repete um pedido que falhou
// por uma razão passageira. Zero desliga as retentativas; negativo é zero.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = max(n, 0) }
}

// WithUserAgent acrescenta um nome ao User-Agent, por exemplo "loja/1.4".
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua + " " + c.userAgent
		}
	}
}

// NewClient cria um cliente com a chave secreta (sk_test_... ou sk_live_...).
func NewClient(secretKey string, opts ...Option) *Client {
	c := &Client{
		key:        secretKey,
		baseURL:    DefaultBaseURL,
		httpClient: http.DefaultClient,
		timeout:    DefaultTimeout,
		maxRetries: DefaultMaxRetries,
		userAgent:  "spolly-payments-go/" + Version,
		dormir:     dormir,
		aleatorio:  mrand.Int64N,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// RequestOption muda uma chamada só.
type RequestOption func(*opcoesDoPedido)

type opcoesDoPedido struct {
	chave string
}

// WithIdempotencyKey dá a Idempotency-Key de um POST, em vez da que o
// cliente gera. A mesma chave com o mesmo corpo devolve a mesma resposta
// durante 24 horas; com outro corpo, a API recusa (CodeIdempotencyReused).
func WithIdempotencyKey(k string) RequestOption {
	return func(o *opcoesDoPedido) { o.chave = k }
}

// NewIdempotencyKey devolve uma chave nova (um UUID v4), como a que o
// cliente gera sozinho.
func NewIdempotencyKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read nunca falha (Go 1.24 ou mais recente)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ErrInvalidResponse é uma resposta da API que não se conseguiu ler: um corpo
// que não é JSON, ou que não tem a forma do objeto pedido. Nunca se devolve,
// em vez dela, um objeto vazio.
var ErrInvalidResponse = errors.New("spolly: a resposta da API não se conseguiu ler")

// ErrNoKey é chamar a API sem chave.
var ErrNoKey = errors.New("spolly: falta a chave secreta (sk_test_... ou sk_live_...)")

// objeto é o que todas as respostas lidas têm de ter.
type objeto interface{ valido() bool }

// pedir faz o pedido, com as retentativas, e lê a resposta para dst.
func (c *Client) pedir(ctx context.Context, metodo, caminho string, corpo any, dst objeto, opts []RequestOption) error {
	if c.key == "" {
		return ErrNoKey
	}
	var o opcoesDoPedido
	for _, f := range opts {
		f(&o)
	}
	var dados []byte
	if corpo != nil {
		// Os parâmetros são tipos deste pacote, só com texto, números, listas
		// e mapas de texto: escrevê-los em JSON não falha.
		dados, _ = json.Marshal(corpo)
	}
	if metodo == http.MethodPost && o.chave == "" {
		o.chave = NewIdempotencyKey()
	}

	base, err := http.NewRequest(metodo, c.baseURL+caminho, nil)
	if err != nil {
		return fmt.Errorf("spolly: o endereço da API não serve: %w", err)
	}
	base.Header.Set("Authorization", "Bearer "+c.key)
	base.Header.Set("Accept", "application/json")
	base.Header.Set("User-Agent", c.userAgent)
	if dados != nil {
		base.Header.Set("Content-Type", "application/json")
	}
	if o.chave != "" {
		base.Header.Set(IdempotencyKeyHeader, o.chave)
	}

	for tentativa := 0; ; tentativa++ {
		estado, cabecalhos, resposta, err := c.tentar(ctx, base, dados)
		if err == nil && estado < 300 {
			if err := json.Unmarshal(resposta, dst); err != nil || !dst.valido() {
				return fmt.Errorf("%w: %s %s deu %d com um corpo que não é o esperado", ErrInvalidResponse, metodo, caminho, estado)
			}
			return nil
		}
		if err == nil && !repetivel(estado) {
			return lerErro(estado, resposta, o.chave)
		}
		if ctx.Err() != nil || tentativa >= c.maxRetries {
			if err != nil {
				return fmt.Errorf("spolly: %s %s: %w", metodo, caminho, err)
			}
			return lerErro(estado, resposta, o.chave)
		}
		if err := c.dormir(ctx, c.espera(tentativa, cabecalhos)); err != nil {
			return fmt.Errorf("spolly: %s %s: %w", metodo, caminho, err)
		}
	}
}

// tentar faz uma tentativa, com o tempo limite dela, e lê o corpo inteiro. O
// pedido é o mesmo em todas as tentativas, com a mesma Idempotency-Key.
func (c *Client) tentar(ctx context.Context, base *http.Request, dados []byte) (int, http.Header, []byte, error) {
	ctx, cancelar := context.WithTimeout(ctx, c.timeout)
	defer cancelar()
	req := base.Clone(ctx)
	if dados != nil {
		req.Body = io.NopCloser(bytes.NewReader(dados))
		req.ContentLength = int64(len(dados))
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResposta))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("ler a resposta: %w", err)
	}
	return res.StatusCode, res.Header, b, nil
}

// repetivel diz se um estado é passageiro. Com a Idempotency-Key, repetir um
// POST é seguro: a API devolve a resposta guardada, ou faz o trabalho uma vez.
func repetivel(estado int) bool {
	switch estado {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// espera é o tempo antes da tentativa seguinte: o Retry-After, se vier e for
// razoável, ou uma espera exponencial com metade aleatória.
func (c *Client) espera(tentativa int, h http.Header) time.Duration {
	if h != nil {
		if s, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && s >= 0 {
			return min(time.Duration(s)*time.Second, retryAfterMaximo)
		}
	}
	d := min(esperaBase<<tentativa, esperaMaxima)
	return d/2 + time.Duration(c.aleatorio(int64(d/2)+1))
}

func dormir(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// caminhoCom junta um identificador ao caminho, escapado.
func caminhoCom(base, id string) string {
	return base + "/" + url.PathEscape(id)
}
