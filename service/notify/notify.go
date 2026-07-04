package notify

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service"
)

// NotificationLevel is the severity of a notification.
type NotificationLevel string

const (
	LevelInfo    NotificationLevel = "info"
	LevelWarning NotificationLevel = "warning"
	LevelError   NotificationLevel = "error"
)

// NotificationChannel is the channel identifier.
type NotificationChannel string

const (
	ChannelWebhook   NotificationChannel = "webhook"
	ChannelBark      NotificationChannel = "bark"
	ChannelServerChan NotificationChannel = "serverchan"
	ChannelTelegram  NotificationChannel = "telegram"
	ChannelSMTP      NotificationChannel = "smtp"
)

// Channel is the interface for notification channels.
type Channel interface {
	Name() string
	Send(cfg *config.Config, title, message, level, timeFootnote string) error
}

// SendNotificationOptions configures notification behavior.
type SendNotificationOptions struct {
	BypassThrottle bool
	RequireChannel bool
	ThrowOnFailure bool
}

// DispatchResult is the result of a notification dispatch.
type DispatchResult struct {
	Throttled      bool
	Attempted      int
	Succeeded      int
	Failed         int
	FailedChannels []NotificationChannel
}

// All notification channels
var channels = []Channel{
	&WebhookChannel{},
	&BarkChannel{},
	&ServerChanChannel{},
	&TelegramChannel{},
	&SMTPChannel{},
}

// SendNotification dispatches a notification through all configured channels.
// Mirrors TS sendNotification().
func SendNotification(cfg *config.Config, title, message, level string, options *SendNotificationOptions) (*DispatchResult, error) {
	if options == nil {
		options = &SendNotificationOptions{}
	}

	now := time.Now()
	timeFootnote := service.BuildTimeFootnote(now)
	cooldownMs := int64(cfg.NotifyCooldownSec) * 1000
	if cooldownMs < 0 {
		cooldownMs = 0
	}

	resolvedMessage := message

	// Throttle check
	if !options.BypassThrottle && cooldownMs > 0 {
		nowMs := time.Now().UnixMilli()
		staleMs := cooldownMs * 6
		if staleMs < 600_000 {
			staleMs = 600_000
		}
		GlobalThrottle.PruneNotificationThrottleState(nowMs, staleMs)

		signature := CreateNotificationSignature(title, message, level)
		decision := GlobalThrottle.EvaluateNotificationThrottle(signature, nowMs, cooldownMs)
		if !decision.ShouldSend {
			return &DispatchResult{
				Throttled:      true,
				Attempted:      0,
				Succeeded:      0,
				Failed:         0,
				FailedChannels: nil,
			}, nil
		}
		if decision.MergedCount > 0 {
			resolvedMessage = fmt.Sprintf("%s\n\n[通知合并] 冷静期内已合并 %d 条重复告警", message, decision.MergedCount)
		}
	}

	// Build task list
	type task struct {
		channel NotificationChannel
		run     func() error
	}

	var tasks []task

	if cfg.WebhookEnabled && cfg.WebhookUrl != "" {
		wh := &WebhookChannel{}
		tasks = append(tasks, task{
			channel: ChannelWebhook,
			run:     func() error { return wh.Send(cfg, title, resolvedMessage, level, timeFootnote) },
		})
	}

	if cfg.BarkEnabled && cfg.BarkUrl != "" {
		bk := &BarkChannel{}
		tasks = append(tasks, task{
			channel: ChannelBark,
			run:     func() error { return bk.Send(cfg, title, resolvedMessage, level, timeFootnote) },
		})
	}

	if cfg.ServerChanEnabled && cfg.ServerChanKey != "" {
		sc := &ServerChanChannel{}
		tasks = append(tasks, task{
			channel: ChannelServerChan,
			run:     func() error { return sc.Send(cfg, title, resolvedMessage, level, timeFootnote) },
		})
	}

	if cfg.TelegramEnabled && cfg.TelegramBotToken != "" && cfg.TelegramChatId != "" {
		tg := &TelegramChannel{}
		tasks = append(tasks, task{
			channel: ChannelTelegram,
			run:     func() error { return tg.Send(cfg, title, resolvedMessage, level, timeFootnote) },
		})
	}

	if cfg.SmtpEnabled && cfg.SmtpHost != "" && cfg.SmtpPort > 0 && cfg.SmtpFrom != "" && cfg.SmtpTo != "" {
		smtpCh := &SMTPChannel{}
		tasks = append(tasks, task{
			channel: ChannelSMTP,
			run:     func() error { return smtpCh.Send(cfg, title, resolvedMessage, level, timeFootnote) },
		})
	}

	// No channels configured
	if len(tasks) == 0 {
		err := fmt.Errorf("no notification channels configured")
		if options.RequireChannel || options.ThrowOnFailure {
			slog.Error("SendNotification: " + err.Error())
			return nil, err
		}
		return &DispatchResult{
			Throttled:      false,
			Attempted:      0,
			Succeeded:      0,
			Failed:         0,
			FailedChannels: nil,
		}, nil
	}

	// Parallel dispatch
	type taskResult struct {
		channel NotificationChannel
		ok      bool
		err     error
	}

	resultCh := make(chan taskResult, len(tasks))
	var wg sync.WaitGroup

	for _, t := range tasks {
		wg.Add(1)
		go func(t task) {
			defer wg.Done()
			err := t.run()
			resultCh <- taskResult{channel: t.channel, ok: err == nil, err: err}
		}(t)
	}
	wg.Wait()
	close(resultCh)

	// Aggregate results
	var failedResults []taskResult
	succeeded := 0
	for r := range resultCh {
		if r.ok {
			succeeded++
		} else {
			failedResults = append(failedResults, r)
		}
	}

	failedChannels := make([]NotificationChannel, 0, len(failedResults))
	for _, fr := range failedResults {
		failedChannels = append(failedChannels, fr.channel)
	}

	if options.ThrowOnFailure && succeeded == 0 && len(failedResults) > 0 {
		firstErr := failedResults[0].err
		slog.Error("SendNotification: all channels failed",
			"first_error", firstErr)
		return nil, firstErr
	}

	return &DispatchResult{
		Throttled:      false,
		Attempted:      len(tasks),
		Succeeded:      succeeded,
		Failed:         len(failedResults),
		FailedChannels: failedChannels,
	}, nil
}
