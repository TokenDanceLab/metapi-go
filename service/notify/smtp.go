package notify

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
)

// SMTPChannel sends notifications via SMTP email.
type SMTPChannel struct {
	mu                sync.Mutex
	cachedFingerprint string
	cachedClient      *cachedSMTPClient
}

type cachedSMTPClient struct {
	addr        string
	auth        smtp.Auth
	from        string
	useTLS      bool
	tlsConfig   *tls.Config
}

func (c *SMTPChannel) Name() string { return "smtp" }

func (c *SMTPChannel) Send(cfg *config.Config, title, message, level, timeFootnote string) error {
	if !cfg.SmtpEnabled || cfg.SmtpHost == "" || cfg.SmtpPort <= 0 || cfg.SmtpFrom == "" || cfg.SmtpTo == "" {
		return fmt.Errorf("smtp not configured")
	}

	fingerprint := getSmtpFingerprint(cfg)

	subject := fmt.Sprintf("[metapi][%s] %s", strings.ToUpper(level), title)
	body := fmt.Sprintf("%s\r\n\r\nLevel: %s\r\n%s", message, level, timeFootnote)
	emailBody := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		cfg.SmtpFrom, cfg.SmtpTo, subject, body)

	addr := fmt.Sprintf("%s:%d", cfg.SmtpHost, cfg.SmtpPort)

	c.mu.Lock()
	// Check if config changed and cached client is invalid
	if c.cachedFingerprint != fingerprint {
		c.cachedFingerprint = fingerprint
		c.cachedClient = nil
	}
	cachedClient := c.cachedClient
	c.mu.Unlock()

	// Use cached client if available and config hasn't changed
	if cachedClient != nil && cachedClient.addr == addr {
		err := smtp.SendMail(addr, cachedClient.auth, cachedClient.from, strings.Split(cfg.SmtpTo, ","), []byte(emailBody))
		if err != nil {
			return fmt.Errorf("smtp send failed: %w", err)
		}
		return nil
	}

	var auth smtp.Auth
	if cfg.SmtpUser != "" {
		auth = smtp.PlainAuth("", cfg.SmtpUser, cfg.SmtpPass, cfg.SmtpHost)
	}

	if cfg.SmtpSecure {
		// Direct TLS connection (SMTPS, typically port 465)
		tlsConfig := &tls.Config{ServerName: cfg.SmtpHost}
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 30 * time.Second}, "tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("smtp tls dial failed: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, cfg.SmtpHost)
		if err != nil {
			return fmt.Errorf("smtp new client failed: %w", err)
		}
		defer client.Close()

		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("smtp auth failed: %w", err)
			}
		}
		if err := client.Mail(cfg.SmtpFrom); err != nil {
			return fmt.Errorf("smtp mail failed: %w", err)
		}
		for _, to := range strings.Split(cfg.SmtpTo, ",") {
			if err := client.Rcpt(strings.TrimSpace(to)); err != nil {
				return fmt.Errorf("smtp rcpt failed: %w", err)
			}
		}
		wc, err := client.Data()
		if err != nil {
			return fmt.Errorf("smtp data failed: %w", err)
		}
		_, err = wc.Write([]byte(emailBody))
		if err != nil {
			wc.Close()
			return fmt.Errorf("smtp write failed: %w", err)
		}
		err = wc.Close()
		if err != nil {
			return fmt.Errorf("smtp close failed: %w", err)
		}
		client.Quit()
	} else {
		// Plain SMTP or STARTTLS (net/smtp.SendMail handles STARTTLS automatically)
		err := smtp.SendMail(addr, auth, cfg.SmtpFrom, strings.Split(cfg.SmtpTo, ","), []byte(emailBody))
		if err != nil {
			return fmt.Errorf("smtp send failed: %w", err)
		}
	}

	// Cache the client config for reuse
	c.mu.Lock()
	c.cachedClient = &cachedSMTPClient{
		addr:   addr,
		auth:   auth,
		from:   cfg.SmtpFrom,
		useTLS: cfg.SmtpSecure,
	}
	c.mu.Unlock()

	return nil
}

func getSmtpFingerprint(cfg *config.Config) string {
	return fmt.Sprintf("%s|%d|%t|%s|%s|%s|%s",
		cfg.SmtpHost, cfg.SmtpPort, cfg.SmtpSecure,
		cfg.SmtpUser, cfg.SmtpPass, cfg.SmtpFrom, cfg.SmtpTo,
	)
}
