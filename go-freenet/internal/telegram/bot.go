// Package telegram implements a Telegram bot for remote FreeNet management.
//
// The bot uses the Telegram Bot API via plain HTTP long-polling (no external
// dependencies beyond the standard library).  It exposes five commands:
//
//	/status   — show current state: enabled/disabled, active strategy
//	/on       — enable DPI bypass
//	/off      — disable DPI bypass
//	/strategy <name> — switch strategy (auto|split|tlsrec|disorder|fake|none)
//	/stats    — show connection counters (active, total, bypassed)
//
// # Security
//
// Anyone who can message the bot can control it.  Keep the token private and
// consider restricting access to a specific chat_id via AllowedChatID.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mintfary-oss/freenet/internal/types"
)

// Controller is the subset of proxy.Server that the bot uses.
// proxy.Server satisfies this interface automatically (compile-time checked
// in bot_test.go via a mock).
type Controller interface {
	Enabled() bool
	SetEnabled(bool)
	Strategy() string
	SetStrategy(string)
	GetStats() types.StatsSnapshot
}

// ---------------------------------------------------------------------------
// Telegram API types (minimal subset)
// ---------------------------------------------------------------------------

type apiUpdate struct {
	UpdateID int         `json:"update_id"`
	Message  *apiMessage `json:"message"`
}

type apiMessage struct {
	MessageID int     `json:"message_id"`
	Chat      apiChat `json:"chat"`
	Text      string  `json:"text"`
}

type apiChat struct {
	ID int64 `json:"id"`
}

type updatesResponse struct {
	OK     bool        `json:"ok"`
	Result []apiUpdate `json:"result"`
}

// ---------------------------------------------------------------------------
// Bot
// ---------------------------------------------------------------------------

// Bot is a long-polling Telegram bot that controls FreeNet remotely.
type Bot struct {
	token         string
	ctrl          Controller
	allowedChatID int64        // 0 = allow all
	client        *http.Client // injected for testing
	baseURL       string       // injected for testing (defaults to Telegram API)
}

// NewBot creates a Bot.  Call Run(ctx) to start long polling.
//
//   - token        — Telegram bot token (from @BotFather)
//   - ctrl         — proxy controller (proxy.Server)
//   - allowedChatID — if non-zero, only this chat_id can control the bot;
//     pass 0 to allow all chats (suitable for private bots)
func NewBot(token string, ctrl Controller, allowedChatID int64) *Bot {
	return &Bot{
		token:         token,
		ctrl:          ctrl,
		allowedChatID: allowedChatID,
		client:        &http.Client{Timeout: 70 * time.Second},
		baseURL:       "https://api.telegram.org",
	}
}

// Run starts the long-polling loop and blocks until ctx is cancelled.
// It logs errors but never returns an error (reconnects automatically).
func (b *Bot) Run(ctx context.Context) {
	log.Printf("telegram: bot starting")
	var offset int
	for {
		select {
		case <-ctx.Done():
			log.Printf("telegram: bot stopped")
			return
		default:
		}

		updates, err := b.getUpdates(ctx, offset, 30)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("telegram: getUpdates error: %v (retrying in 5s)", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.Message == nil {
				continue
			}
			b.handleMessage(ctx, u.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// Message handler
// ---------------------------------------------------------------------------

// handleMessage dispatches a command message to the appropriate handler.
func (b *Bot) handleMessage(ctx context.Context, msg *apiMessage) {
	chatID := msg.Chat.ID
	if b.allowedChatID != 0 && chatID != b.allowedChatID {
		// Silently ignore unauthorised senders.
		return
	}

	text := strings.TrimSpace(msg.Text)
	// Strip the bot username suffix if present (e.g. /status@MyBot).
	if idx := strings.Index(text, "@"); idx != -1 && strings.HasPrefix(text, "/") {
		text = text[:idx]
	}

	// Dispatch.
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	var reply string
	switch cmd {
	case "/start", "/help":
		reply = b.cmdHelp()
	case "/status":
		reply = b.cmdStatus()
	case "/on":
		reply = b.cmdSetEnabled(true)
	case "/off":
		reply = b.cmdSetEnabled(false)
	case "/strategy":
		reply = b.cmdStrategy(args)
	case "/stats":
		reply = b.cmdStats()
	default:
		reply = "Неизвестная команда. Используй /help."
	}

	if err := b.sendMessage(ctx, chatID, reply); err != nil {
		log.Printf("telegram: sendMessage to %d: %v", chatID, err)
	}
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

// cmdHelp returns the help text.
func (b *Bot) cmdHelp() string {
	return "FreeNet — управление обходом DPI\n\n" +
		"/status — текущее состояние\n" +
		"/on — включить обход\n" +
		"/off — выключить обход\n" +
		"/strategy <имя> — сменить стратегию\n" +
		"  доступны: auto split tlsrec disorder fake combined none\n" +
		"/stats — статистика соединений"
}

// cmdStatus returns the current state of the proxy.
func (b *Bot) cmdStatus() string {
	state := "ВЫКЛЮЧЕН ❌"
	if b.ctrl.Enabled() {
		state = "ВКЛЮЧЁН ✅"
	}
	return fmt.Sprintf("FreeNet: %s\nСтратегия: %s", state, b.ctrl.Strategy())
}

// cmdSetEnabled enables or disables the bypass and returns a confirmation.
func (b *Bot) cmdSetEnabled(on bool) string {
	b.ctrl.SetEnabled(on)
	if on {
		return fmt.Sprintf("Обход включён ✅ (стратегия: %s)", b.ctrl.Strategy())
	}
	return "Обход выключен ❌"
}

// validStrategies is the set of strategy names accepted by /strategy.
var validStrategies = map[string]bool{
	"auto":     true,
	"split":    true,
	"tlsrec":   true,
	"disorder": true,
	"fake":     true,
	"combined": true,
	"none":     true,
}

// cmdStrategy changes the bypass strategy.
func (b *Bot) cmdStrategy(args []string) string {
	if len(args) == 0 {
		return fmt.Sprintf("Текущая стратегия: %s\n\nИспользование: /strategy <имя>\nДоступны: auto split tlsrec disorder fake combined none", b.ctrl.Strategy())
	}
	name := strings.ToLower(args[0])
	if !validStrategies[name] {
		return fmt.Sprintf("Неизвестная стратегия %q.\nДоступны: auto split tlsrec disorder fake combined none", name)
	}
	b.ctrl.SetStrategy(name)
	return fmt.Sprintf("Стратегия изменена на: %s ✅", name)
}

// cmdStats returns a human-readable snapshot of the proxy counters.
func (b *Bot) cmdStats() string {
	s := b.ctrl.GetStats()
	return fmt.Sprintf(
		"Статистика соединений:\n"+
			"Активных: %d\n"+
			"Всего:    %d\n"+
			"Обойдено: %d\n"+
			"Прямых:   %d",
		s.Active, s.Total, s.Bypassed, s.Passthrough,
	)
}

// ---------------------------------------------------------------------------
// Telegram API helpers
// ---------------------------------------------------------------------------

// apiURL builds a full Telegram Bot API URL.
func (b *Bot) apiURL(method string) string {
	return fmt.Sprintf("%s/bot%s/%s", b.baseURL, b.token, method)
}

// getUpdates calls getUpdates with long-poll timeout seconds.
func (b *Bot) getUpdates(ctx context.Context, offset, timeout int) ([]apiUpdate, error) {
	apiURL := b.apiURL("getUpdates")
	params := url.Values{
		"offset":  []string{fmt.Sprintf("%d", offset)},
		"timeout": []string{fmt.Sprintf("%d", timeout)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result updatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram API returned ok=false")
	}
	return result.Result, nil
}

// sendMessage sends a text message to chatID.
func (b *Bot) sendMessage(ctx context.Context, chatID int64, text string) error {
	body, _ := json.Marshal(map[string]any{
		"chat_id": chatID,
		"text":    text,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.apiURL("sendMessage"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sendMessage: HTTP %d", resp.StatusCode)
	}
	return nil
}
