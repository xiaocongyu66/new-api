package model

import (
	"github.com/QuantumNous/new-api/model"
)

type tokenResolver struct{}

func (tokenResolver) GetTokenById(id int) (string, bool) {
	tok, err := GetTokenById(id)
	if err != nil || tok == nil {
		return "", false
	}
	return tok.Name, true
}

func init() {
	model.RegisterTokenResolver(tokenResolver{})
}
