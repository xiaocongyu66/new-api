package middleware

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/service"
)

// BodyStorageCleanup 请求体存储清理中间件
// 在请求处理完成后自动清理磁盘/内存缓存
func BodyStorageCleanup() contract.Middleware {
	return func(c contract.Context) {
		// 处理请求
		c.Next()

		// 请求结束后清理存储
		common.CleanupBodyStorage(c)

		// 清理文件缓存（URL 下载的文件等）
		service.CleanupFileSources(c)
	}
}
