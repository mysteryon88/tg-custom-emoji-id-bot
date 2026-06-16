package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	telegramAPIBase = "https://api.telegram.org"
	pollTimeoutSec  = 30
)

var telegramBotTokenPattern = regexp.MustCompile(`bot[0-9]+:[A-Za-z0-9_-]+`)

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	Chat            Chat            `json:"chat"`
	Text            string          `json:"text,omitempty"`
	Entities        []MessageEntity `json:"entities,omitempty"`
	Caption         string          `json:"caption,omitempty"`
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type MessageEntity struct {
	Type          string `json:"type"`
	Offset        int    `json:"offset"`
	Length        int    `json:"length"`
	CustomEmojiID string `json:"custom_emoji_id,omitempty"`
}

type customEmoji struct {
	Emoji         string
	CustomEmojiID string
}

type telegramClient struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

type telegramResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	token, err := readBotToken()
	if err != nil {
		log.Fatal(err)
	}

	proxyURL, err := readProxyURL()
	if err != nil {
		log.Fatal(err)
	}

	client, err := newTelegramClient(token, proxyURL)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("bot started")
	if proxyURL != "" {
		log.Println("telegram proxy enabled")
	}

	if err := pollUpdates(ctx, client); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func readBotToken() (string, error) {
	if err := loadDotEnv(".env"); err != nil {
		return "", fmt.Errorf("failed to read .env")
	}

	token := strings.TrimSpace(os.Getenv("BOT_TOKEN"))
	if token == "" {
		return "", errors.New("BOT_TOKEN is not set")
	}

	return token, nil
}

func readProxyURL() (string, error) {
	return strings.TrimSpace(os.Getenv("BOT_PROXY_URL")), nil
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func newTelegramClient(token string, proxyRawURL string) (*telegramClient, error) {
	httpClient, err := newHTTPClient(proxyRawURL)
	if err != nil {
		return nil, err
	}

	return &telegramClient{
		token:      token,
		baseURL:    telegramAPIBase,
		httpClient: httpClient,
	}, nil
}

func newHTTPClient(proxyRawURL string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if proxyRawURL != "" {
		proxyURL, err := url.Parse(proxyRawURL)
		if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
			return nil, fmt.Errorf("BOT_PROXY_URL is invalid")
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	return &http.Client{
		Timeout:   time.Duration(pollTimeoutSec+5) * time.Second,
		Transport: transport,
	}, nil
}

func pollUpdates(ctx context.Context, client *telegramClient) error {
	var offset int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, err := client.getUpdates(ctx, offset)
		if err != nil {
			log.Printf("failed to get updates: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			handleUpdate(ctx, client, update)
		}
	}
}

func (c *telegramClient) getUpdates(ctx context.Context, offset int64) ([]Update, error) {
	values := url.Values{}
	values.Set("timeout", strconv.Itoa(pollTimeoutSec))
	values.Set("allowed_updates", `["message"]`)
	if offset > 0 {
		values.Set("offset", strconv.FormatInt(offset, 10))
	}

	return postTelegramForm[[]Update](ctx, c, "getUpdates", values)
}

func (c *telegramClient) SendMessage(ctx context.Context, chatID int64, text string) error {
	values := url.Values{}
	values.Set("chat_id", strconv.FormatInt(chatID, 10))
	values.Set("text", text)
	values.Set("parse_mode", "MarkdownV2")

	_, err := postTelegramForm[json.RawMessage](ctx, c, "sendMessage", values)
	return err
}

func postTelegramForm[T any](ctx context.Context, client *telegramClient, method string, values url.Values) (T, error) {
	var zero T

	requestURL := fmt.Sprintf("%s/bot%s/%s", client.baseURL, client.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(values.Encode()))
	if err != nil {
		return zero, fmt.Errorf("failed to create Telegram API request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return zero, fmt.Errorf("Telegram API request failed: %s", safeRequestError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return zero, fmt.Errorf("Telegram API returned HTTP %d", resp.StatusCode)
	}

	var parsed telegramResponse[T]
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return zero, fmt.Errorf("failed to decode Telegram API response")
	}
	if !parsed.OK {
		if parsed.Description != "" {
			return zero, fmt.Errorf("Telegram API error %d: %s", parsed.ErrorCode, parsed.Description)
		}
		return zero, fmt.Errorf("Telegram API error %d", parsed.ErrorCode)
	}

	return parsed.Result, nil
}

func safeRequestError(err error) string {
	if err == nil {
		return "unknown network error"
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return redactTelegramBotToken(urlErr.Err.Error())
	}

	return redactTelegramBotToken(err.Error())
}

func redactTelegramBotToken(text string) string {
	return telegramBotTokenPattern.ReplaceAllString(text, "bot<redacted>")
}

type messageSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

func handleUpdate(ctx context.Context, sender messageSender, update Update) {
	if update.Message == nil {
		return
	}

	message := update.Message
	command := commandName(message.Text)
	var response string

	switch command {
	case "start":
		response = startResponse()
	case "help":
		response = helpResponse()
	default:
		items := extractCustomEmojis(message.Text, message.Entities)
		items = append(items, extractCustomEmojis(message.Caption, message.CaptionEntities)...)
		if len(items) == 0 {
			response = noCustomEmojiResponse()
		} else {
			response = buildResponse(items)
		}
	}

	if err := sender.SendMessage(ctx, message.Chat.ID, response); err != nil {
		log.Printf("failed to send message: %v", err)
	}
}

func commandName(text string) string {
	if !strings.HasPrefix(text, "/") {
		return ""
	}

	firstField := strings.Fields(text)
	if len(firstField) == 0 {
		return ""
	}

	command := strings.TrimPrefix(firstField[0], "/")
	if at := strings.Index(command, "@"); at >= 0 {
		command = command[:at]
	}

	return command
}

func extractCustomEmojis(text string, entities []MessageEntity) []customEmoji {
	var result []customEmoji

	for _, entity := range entities {
		if entity.Type != "custom_emoji" || entity.CustomEmojiID == "" {
			continue
		}

		emoji := substringByUTF16Range(text, entity.Offset, entity.Length)
		if emoji == "" {
			continue
		}

		result = append(result, customEmoji{
			Emoji:         emoji,
			CustomEmojiID: entity.CustomEmojiID,
		})
	}

	return result
}

func substringByUTF16Range(text string, offset int, length int) string {
	if offset < 0 || length <= 0 {
		return ""
	}

	targetEnd := offset + length
	units := 0
	startByte := -1
	endByte := -1

	for byteIndex, r := range text {
		if units == offset {
			startByte = byteIndex
		}
		if units == targetEnd {
			endByte = byteIndex
			break
		}

		runeUnits := 1
		if r > 0xFFFF {
			runeUnits = 2
		}
		nextUnits := units + runeUnits

		if (offset > units && offset < nextUnits) || (targetEnd > units && targetEnd < nextUnits) {
			return ""
		}

		units = nextUnits
	}

	if startByte < 0 && units == offset {
		startByte = len(text)
	}
	if endByte < 0 && units == targetEnd {
		endByte = len(text)
	}
	if startByte < 0 || endByte < 0 || startByte > endByte {
		return ""
	}

	return text[startByte:endByte]
}

func buildResponse(items []customEmoji) string {
	var b strings.Builder

	b.WriteString("*Found custom emoji:* ")
	b.WriteString(strconv.Itoa(len(items)))
	b.WriteString("\n\n")

	for i, item := range items {
		if i > 0 {
			b.WriteString("\n\n")
		}

		b.WriteString("*Custom emoji ")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString("*\n\n")
		b.WriteString("Emoji:\n")
		b.WriteString(escapeMarkdownV2(item.Emoji))
		b.WriteString("\n\n")
		b.WriteString("ID:\n`")
		b.WriteString(escapeMarkdownV2Code(item.CustomEmojiID))
		b.WriteString("`\n\n")

		htmlVariant := fmt.Sprintf(`<tg-emoji emoji-id="%s">%s</tg-emoji>`, item.CustomEmojiID, item.Emoji)
		markdownVariant := fmt.Sprintf(`![%s](tg://emoji?id=%s)`, item.Emoji, item.CustomEmojiID)

		b.WriteString("HTML:\n`")
		b.WriteString(escapeMarkdownV2Code(htmlVariant))
		b.WriteString("`\n\n")
		b.WriteString("MarkdownV2:\n`")
		b.WriteString(escapeMarkdownV2Code(markdownVariant))
		b.WriteString("`")
	}

	return b.String()
}

func startResponse() string {
	return escapeMarkdownV2("Hi! Send me a message with one or more Telegram custom emoji, and I will return their custom_emoji_id plus HTML and MarkdownV2 snippets.")
}

func helpResponse() string {
	htmlExample := `<tg-emoji emoji-id="123456">🧡</tg-emoji>`
	markdownExample := `![🧡](tg://emoji?id=123456)`

	return escapeMarkdownV2("Send custom emoji in a text message or in a media caption. If the message contains several custom emoji, I will render each one as a separate block.") +
		"\n\n*Example output:*\n\n" +
		"Emoji:\n🧡\n\n" +
		"ID:\n`123456`\n\n" +
		"HTML:\n`" + escapeMarkdownV2Code(htmlExample) + "`\n\n" +
		"MarkdownV2:\n`" + escapeMarkdownV2Code(markdownExample) + "`"
}

func noCustomEmojiResponse() string {
	return escapeMarkdownV2("Send me a Telegram custom emoji, and I will show its ID. You can use a text message or a media caption.")
}

func escapeMarkdownV2(text string) string {
	const special = "_*[]()~`>#+-=|{}.!" + "\\"

	var b strings.Builder
	for _, r := range text {
		if strings.ContainsRune(special, r) {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}

	return b.String()
}

func escapeMarkdownV2Code(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r == '`' || r == '\\' {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}

	return b.String()
}
