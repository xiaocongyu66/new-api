package middleware

import (
	"compress/gzip"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/internal/common"
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

// maxBytesReadCloser reports an oversized stream instead of silently truncating
// it. The limit is probed one byte past the cap so an exact-limit body reaches
// EOF normally.
type maxBytesReadCloser struct {
	io.ReadCloser
	remaining int64
}

func (r *maxBytesReadCloser) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining <= 0 {
		var probe [1]byte
		n, err := r.ReadCloser.Read(probe[:])
		if n > 0 {
			return 0, common.ErrRequestBodyTooLarge
		}
		if err == nil {
			err = io.EOF
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.ReadCloser.Read(p)
	r.remaining -= int64(n)
	return n, err
}

// DecompressRequestMiddleware swaps a compressed request body for a decompressed
// one and caps the post-decompression size.
//
// The cap wraps the raw stream before anything reads it. BodyReader buffers the
// still-compressed bytes, which would let a zip bomb expand past the cap.
func DecompressRequestMiddleware() contract.Middleware {
	return func(c contract.Context) {
		origBody := c.BodyStream()
		if origBody == nil || c.Method() == http.MethodGet {
			c.Next()
			return
		}
		maxMB := constant.MaxRequestBodyMB
		if maxMB <= 0 {
			maxMB = 32
		}
		maxBytes := int64(maxMB) << 20

		wrapMaxBytes := func(body io.ReadCloser) io.ReadCloser {
			return &maxBytesReadCloser{ReadCloser: body, remaining: maxBytes}
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
