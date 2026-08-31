package middleware

import (
	"compress/gzip"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

type readCloser struct {
	io.Reader
	closeFn func() error
}

func (rc *readCloser) Close() error {
	if rc.closeFn != nil {
		return rc.closeFn()
	}
	return nil
}

// DecompressRequestMiddleware swaps a compressed request body for a decompressed
// one and caps the post-decompression size.
//
// It reaches through the contract's standard-library escape hatches rather than
// the buffered body accessors on purpose: the cap has to wrap the stream before
// anything reads it, and http.MaxBytesReader needs the response writer so an
// oversized body fails the request instead of being silently truncated.
func DecompressRequestMiddleware() contract.Middleware {
	return func(c contract.Context) {
		request := c.HTTPRequest()
		if request.Body == nil || c.Method() == http.MethodGet {
			c.Next()
			return
		}
		maxMB := constant.MaxRequestBodyMB
		if maxMB <= 0 {
			maxMB = 32
		}
		maxBytes := int64(maxMB) << 20

		origBody := request.Body
		wrapMaxBytes := func(body io.ReadCloser) io.ReadCloser {
			return http.MaxBytesReader(c.ResponseWriter(), body, maxBytes)
		}
		decompressed := false

		switch c.Header("Content-Encoding") {
		case "gzip":
			gzipReader, err := gzip.NewReader(origBody)
			if err != nil {
				_ = origBody.Close()
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			// Replace the request body with the decompressed data, and enforce a max size (post-decompression).
			c.ResetBody(wrapMaxBytes(&readCloser{
				Reader: gzipReader,
				closeFn: func() error {
					_ = gzipReader.Close()
					return origBody.Close()
				},
			}))
			decompressed = true
		case "br":
			reader := brotli.NewReader(origBody)
			c.ResetBody(wrapMaxBytes(&readCloser{
				Reader: reader,
				closeFn: func() error {
					return origBody.Close()
				},
			}))
			decompressed = true
		case "zstd":
			reader, err := zstd.NewReader(origBody)
			if err != nil {
				_ = origBody.Close()
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			c.ResetBody(wrapMaxBytes(&readCloser{
				Reader: reader,
				closeFn: func() error {
					reader.Close()
					return origBody.Close()
				},
			}))
			decompressed = true
		default:
			// Even for uncompressed bodies, enforce a max size to avoid huge request allocations.
			c.ResetBody(wrapMaxBytes(origBody))
		}

		if decompressed {
			c.Headers().Del("Content-Encoding")
		}

		// Continue processing the request
		c.Next()
	}
}
