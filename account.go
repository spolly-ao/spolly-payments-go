package spolly

import (
	"context"
	"net/http"
	"strings"
)

// Account é o comerciante e o modo de uma chave.
type Account struct {
	MerchantID string `json:"merchant_id"`
	// TradeName é o nome comercial.
	TradeName string `json:"nome_comercial"`
	Mode      Mode   `json:"modo"`
}

func (a *Account) valido() bool {
	return strings.HasPrefix(a.MerchantID, "mer_") && (a.Mode == ModeTest || a.Mode == ModeLive)
}

// GetAccount diz de que comerciante e de que modo é a chave (GET /v1/conta).
// É a primeira coisa a chamar depois de configurar uma chave: confirma que
// uma sk_test_ não foi parar a produção, e o contrário.
func (c *Client) GetAccount(ctx context.Context) (*Account, error) {
	var a Account
	if err := c.pedir(ctx, http.MethodGet, "/v1/conta", nil, &a, nil); err != nil {
		return nil, err
	}
	return &a, nil
}
