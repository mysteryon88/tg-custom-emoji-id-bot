# Telegram Custom Emoji ID Bot

Small Go bot that extracts Telegram `custom_emoji_id` values from custom emoji in text messages and media captions.

The bot does not store messages, user IDs, tokens, or send data to any external service except the Telegram Bot API.

## Features

- Reads custom emoji entities from message text and captions.
- Returns the fallback emoji, `custom_emoji_id`, HTML snippet, and MarkdownV2 snippet.
- Loads `BOT_TOKEN` from `.env` or the process environment.
- Supports optional Telegram API proxy configuration.

## Create A Bot

1. Open Telegram and find `@BotFather`.
2. Send `/newbot`.
3. Choose a display name.
4. Choose a username ending in `bot`, for example `my_custom_emoji_id_bot`.
5. Copy the token returned by BotFather.

If you need to rotate the token later, use `/token` in BotFather and select the bot.

Never publish the token and never commit `.env`.

## Configuration

Copy the example file:

```bash
cp .env.example .env
```

Set the bot token:

```env
BOT_TOKEN=replace_me_with_your_bot_token
```

If Telegram Bot API is not available directly from your network, add a proxy:

```env
BOT_PROXY_URL=http://127.0.0.1:10809
```

Supported proxy schemes are `http`, `https`, `socks5`, and `socks5h`. If `BOT_PROXY_URL` is not set, Go also respects the standard `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` environment variables.

You can also set environment variables manually:

```bash
export BOT_TOKEN="replace_me_with_your_bot_token"
export BOT_PROXY_URL="http://127.0.0.1:10809"
```

## Run

```bash
go run .
```

## Test

```bash
go test ./...
```

## Usage

1. Start the bot locally.
2. Open the bot in Telegram.
3. Send one or more Telegram custom emoji in a message or media caption.
4. The bot replies with a copy-friendly block:

```text
*Found custom emoji:* 1

*Custom emoji 1*

Emoji:
EMOJI

ID:
`123456`

HTML:
`<tg-emoji emoji-id="123456">EMOJI</tg-emoji>`

MarkdownV2:
`![EMOJI](tg://emoji?id=123456)`
```

If a message contains no custom emoji, the bot replies with a short usage hint.
