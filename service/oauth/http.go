package oauth

import (
	"fmt"
	"io"
)

const (
	oauthJSONResponseBodyLimit  = 1 << 20
	oauthErrorResponseBodyLimit = 64 << 10
)

func readOAuthJSONResponseBody(body io.Reader) ([]byte, error) {
	return readOAuthResponseBody(body, oauthJSONResponseBodyLimit)
}

func readOAuthErrorResponseBody(body io.Reader) []byte {
	data, err := io.ReadAll(io.LimitReader(body, oauthErrorResponseBodyLimit))
	if err != nil {
		return []byte(err.Error())
	}
	return data
}

func readOAuthResponseBody(body io.Reader, limit int64) ([]byte, error) {
	limited := &io.LimitedReader{R: body, N: limit + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("oauth response read failed: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("oauth response body exceeds %d bytes", limit)
	}
	return data, nil
}
