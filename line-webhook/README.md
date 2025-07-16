# LINE OA Webhook + ChatGPT Bot (Go Fiber)

This is a lean Go project using Fiber for a fast webhook to receive LINE OA messages, forward them to a ChatGPT API, and reply to users quickly.

## How it works

- Receives POST requests from LINE OA at `/webhook`
- For each text message, calls your ChatGPT API
- Replies to the user via LINE Messaging API

## Setup

1. Set environment variables:
   - `LINE_CHANNEL_ACCESS_TOKEN` (from LINE Developers Console)
   - `CHATGPT_API_URL` (your ChatGPT endpoint)
   - `CHATGPT_API_KEY` (your ChatGPT API key)
2. Run the server:
   ```powershell
   cd line-webhook
   go mod tidy
   go run main.go
   ```
3. Set your LINE webhook URL to `https://your-server/webhook`

## Dependencies

- [Fiber](https://github.com/gofiber/fiber)

## Reference

- [LINE Messaging API: Receiving messages](https://developers.line.biz/en/docs/messaging-api/receiving-messages/)
