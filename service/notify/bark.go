package notify

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/tokendancelab/metapi-go/config"
)

// BarkChannel sends notifications via Bark.
type BarkChannel struct{}

func (c *BarkChannel) Name() string { return "bark" }

func (c *BarkChannel) Send(cfg *config.Config, title, message, level, timeFootnote string) error {
	if !cfg.BarkEnabled || cfg.BarkUrl == "" {
		return fmt.Errorf("bark not configured")
	}

	barkBase := strings.TrimRight(cfg.BarkUrl, "/")
	reqURL := fmt.Sprintf("%s/%s/%s?group=AllApiHub&level=%s",
		barkBase,
		url.QueryEscape(title),
		url.QueryEscape(message),
		url.QueryEscape(level),
	)

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("bark request build failed: %w", err)
	}
	resp, err := doNotifyRequest(req)
	if err != nil {
		return fmt.Errorf("bark request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("bark response status %d", resp.StatusCode)
	}
	return nil
}
