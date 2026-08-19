package model

import (
	"github.com/QuantumNous/new-api/model"
)

type channelResolver struct{}

func (channelResolver) CacheGetChannel(id int) (string, bool) {
	ch, err := CacheGetChannel(id)
	if err != nil || ch == nil {
		return "", false
	}
	return ch.Name, true
}

func init() {
	model.RegisterChannelResolver(channelResolver{})
}
