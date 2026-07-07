package notify

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/tokendancelab/metapi-go/config"
)

// ServerChanChannel sends notifications via Server酱.
type ServerChanChannel struct{}

func (c *ServerChanChannel) Name() string { return "serverchan" }

func (c *ServerChanChannel) Send(cfg *config.Config, title, message, level, timeFootnote string) error {
	if !cfg.ServerChanEnabled || cfg.ServerChanKey == "" {
		return fmt.Errorf("serverchan not configured")
	}

	desp := fmt.Sprintf("%s\n\nLevel: %s\n%s", message, level, timeFootnote)
	formData := url.Values{
		"title": {title},
		"desp":  {desp},
	}

	reqURL := fmt.Sprintf("https://sctapi.ftqq.com/%s.send", cfg.ServerChanKey)
	req, err := http.NewRequest(http.MethodPost, reqURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return fmt.Errorf("serverchan request build failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := doNotifyRequest(req)
	if err != nil {
		return fmt.Errorf("serverchan request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("serverchan response status %d", resp.StatusCode)
	}
	return nil
}
