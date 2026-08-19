package model

import (
	"github.com/QuantumNous/new-api/model"
)

func init() {
	model.RegisterEntities(
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
	)
}
