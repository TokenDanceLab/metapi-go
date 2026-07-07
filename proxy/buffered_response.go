package proxy

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const DefaultMaxBufferedResponseBodyBytes int64 = 20 << 20

var ErrBufferedResponseBodyTooLarge = errors.New("upstream response body exceeded buffered limit")

func MaxBufferedResponseBodyBytes() int64 {
	raw := strings.TrimSpace(os.Getenv("PROXY_MAX_BUFFERED_RESPONSE_BYTES"))
	if raw == "" {
		return DefaultMaxBufferedResponseBodyBytes
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return DefaultMaxBufferedResponseBodyBytes
	}
	return n
}

func ReadBufferedResponseBody(r io.Reader) ([]byte, error) {
	limit := MaxBufferedResponseBodyBytes()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: limit %d bytes", ErrBufferedResponseBodyTooLarge, limit)
	}
	return data, nil
}
