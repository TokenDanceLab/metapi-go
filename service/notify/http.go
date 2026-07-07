package notify

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	notifyHTTPTimeout         = 10 * time.Second
	notifyMaxResponseBodySize = 64 << 10
)

var notifyHTTPClient = newNotifyHTTPClient()

func newNotifyHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   notifyHTTPTimeout,
		Transport: newNotifyHTTPTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after %d notification redirects", len(via))
			}
			return validateNotifyRequestURL(req.URL)
		},
	}
}

func newNotifyHTTPTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if err := rejectUnsafeNotifyDialHost(ctx, host); err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, address)
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: notifyHTTPTimeout,
		IdleConnTimeout:       30 * time.Second,
	}
}

func doNotifyRequest(req *http.Request) (*http.Response, error) {
	if err := validateNotifyRequestURL(req.URL); err != nil {
		return nil, err
	}
	resp, err := notifyHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func readNotifyResponseBody(body io.Reader) ([]byte, error) {
	limited := io.LimitReader(body, notifyMaxResponseBodySize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > notifyMaxResponseBodySize {
		return nil, fmt.Errorf("notification response body exceeds %d bytes", notifyMaxResponseBodySize)
	}
	return data, nil
}

func validateNotifyRequestURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("notification URL is empty")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("notification URL scheme %q is not allowed", u.Scheme)
	}
	if u.Host == "" || u.User != nil {
		return fmt.Errorf("notification URL host is invalid")
	}
	if port := u.Port(); port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return fmt.Errorf("notification URL port is invalid: %w", err)
		}
	}
	if !isAllowedNotifyTargetHost(u.Hostname()) {
		return fmt.Errorf("refusing notification request to unsafe host %q", u.Hostname())
	}
	return nil
}

func rejectUnsafeNotifyDialHost(ctx context.Context, host string) error {
	if !isAllowedNotifyTargetHost(host) {
		return fmt.Errorf("refusing notification request to unsafe host %q", host)
	}
	if _, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return err
	}
	if len(ips) == 0 {
		return fmt.Errorf("no IP addresses found for notification host %q", host)
	}
	for _, ip := range ips {
		if isUnsafeNotifyAddr(ip) {
			return fmt.Errorf("refusing notification request to unsafe resolved address %s", ip)
		}
	}
	return nil
}

func isAllowedNotifyTargetHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" || strings.Contains(host, "%") {
		return false
	}
	lower := strings.TrimSuffix(strings.ToLower(host), ".")
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return !isUnsafeNotifyAddr(addr)
	}
	return true
}

func isUnsafeNotifyAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast()
}
