package proxy

import (
	"github.com/tokendancelab/metapi-go/proxy"
)

// DetectClientContext is a thin wrapper around proxy.DetectClientContext.
// It converts http.Header to a map[string]string suitable for detection.
func DetectClientContext(downstreamPath string, headers map[string]string, body any) proxy.DownstreamClientContext {
	return proxy.DetectClientContext(downstreamPath, headers, body)
}

// HeaderMapFromRequest extracts relevant headers as a string map for client detection.
func HeaderMapFromRequest(headers map[string][]string) map[string]string {
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	return result
}
