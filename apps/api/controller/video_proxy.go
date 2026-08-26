package controller

import (
	"github.com/QuantumNous/new-api/internal/task"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// VideoProxy delegates to the task capability to proxy video content for completed tasks.
func VideoProxy(c contract.Context) {
	task.VideoProxy(c)
}
