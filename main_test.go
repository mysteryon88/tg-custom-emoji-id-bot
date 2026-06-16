package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestExtractCustomEmojisUsesUTF16Offsets(t *testing.T) {
	text := "A 🧡 Z ⭐"
	entities := []MessageEntity{
		{Type: "custom_emoji", Offset: 2, Length: 2, CustomEmojiID: "111"},
		{Type: "bold", Offset: 5, Length: 1},
		{Type: "custom_emoji", Offset: 7, Length: 1, CustomEmojiID: "222"},
	}

	got := extractCustomEmojis(text, entities)

	if len(got) != 2 {
		t.Fatalf("expected 2 custom emojis, got %d", len(got))
	}
	if got[0].Emoji != "🧡" || got[0].CustomEmojiID != "111" {
		t.Fatalf("unexpected first custom emoji: %#v", got[0])
	}
	if got[1].Emoji != "⭐" || got[1].CustomEmojiID != "222" {
		t.Fatalf("unexpected second custom emoji: %#v", got[1])
	}
}

func TestBuildResponseContainsEscapedMarkdownV2Blocks(t *testing.T) {
	items := []customEmoji{
		{Emoji: "🧡", CustomEmojiID: "123456"},
	}

	got := buildResponse(items)
	want := strings.Join([]string{
		"*Found custom emoji:* 1",
		"",
		"*Custom emoji 1*",
		"",
		"Emoji:",
		"🧡",
		"",
		"ID:",
		"`123456`",
		"",
		"HTML:",
		"`<tg-emoji emoji-id=\"123456\">🧡</tg-emoji>`",
		"",
		"MarkdownV2:",
		"`![🧡](tg://emoji?id=123456)`",
	}, "\n")

	if got != want {
		t.Fatalf("unexpected response:\nwant:\n%s\n\ngot:\n%s", want, got)
	}
	if containsCyrillic(got) {
		t.Fatalf("response contains Cyrillic text:\n%s", got)
	}
}

func TestHandleUpdateExtractsCustomEmojiFromCaption(t *testing.T) {
	sender := &captureSender{}
	update := Update{
		Message: &Message{
			Chat:    Chat{ID: 42},
			Caption: "Photo 🧡",
			CaptionEntities: []MessageEntity{
				{Type: "custom_emoji", Offset: 6, Length: 2, CustomEmojiID: "caption-id"},
			},
		},
	}

	handleUpdate(context.Background(), sender, update)

	if sender.calls != 1 {
		t.Fatalf("expected one sent message, got %d", sender.calls)
	}
	if sender.chatID != 42 {
		t.Fatalf("unexpected chat id: %d", sender.chatID)
	}
	if !strings.Contains(sender.text, "caption-id") || !strings.Contains(sender.text, "`![🧡](tg://emoji?id=caption-id)`") {
		t.Fatalf("caption custom emoji was not rendered in response:\n%s", sender.text)
	}
}

func TestUserFacingResponsesDoNotContainCyrillicText(t *testing.T) {
	responses := map[string]string{
		"start":           startResponse(),
		"help":            helpResponse(),
		"no custom emoji": noCustomEmojiResponse(),
	}

	for name, response := range responses {
		if containsCyrillic(response) {
			t.Fatalf("%s response contains Cyrillic text:\n%s", name, response)
		}
	}
}

func TestNewTelegramClientUsesExplicitProxyURL(t *testing.T) {
	client, err := newTelegramClient("test-token", "http://127.0.0.1:18080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.httpClient.Transport)
	}

	req := httptestRequest(t, "https://api.telegram.org/bottest/getUpdates")
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("unexpected proxy error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:18080" {
		t.Fatalf("unexpected proxy URL: %v", proxyURL)
	}
}

func TestNewTelegramClientRejectsInvalidProxyURL(t *testing.T) {
	if _, err := newTelegramClient("test-token", "://bad-proxy-url"); err == nil {
		t.Fatal("expected invalid proxy URL error")
	}
}

func TestSafeRequestErrorDoesNotExposeBotToken(t *testing.T) {
	err := &url.Error{
		Op:  "Post",
		URL: "https://api.telegram.org/bot123456:secret-token/getUpdates",
		Err: errors.New("dial tcp: connectex: connection refused"),
	}

	got := safeRequestError(err)

	if strings.Contains(got, "123456:secret-token") || strings.Contains(got, "/bot") {
		t.Fatalf("error exposes bot token or Telegram bot URL: %q", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Fatalf("error lost useful network detail: %q", got)
	}
}

func TestEscapeMarkdownV2EscapesSpecialCharacters(t *testing.T) {
	input := `_ * [ ] ( ) ~ ` + "`" + ` > # + - = | { } . ! \`
	want := `\_ \* \[ \] \( \) \~ \` + "`" + ` \> \# \+ \- \= \| \{ \} \. \! \\`

	if got := escapeMarkdownV2(input); got != want {
		t.Fatalf("unexpected escaping:\nwant %q\n got %q", want, got)
	}
}

type captureSender struct {
	calls  int
	chatID int64
	text   string
}

func httptestRequest(t *testing.T, rawURL string) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, rawURL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	return req
}

func (s *captureSender) SendMessage(_ context.Context, chatID int64, text string) error {
	s.calls++
	s.chatID = chatID
	s.text = text
	return nil
}

func containsCyrillic(text string) bool {
	for _, r := range text {
		if r >= '\u0400' && r <= '\u04FF' {
			return true
		}
	}
	return false
}
