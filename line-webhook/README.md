# LINE OA Webhook + ChatGPT Bot (Go Fiber)

This is a lean Go project using Fiber for a fast webhook to receive LINE OA messages, forward them to a ChatGPT API, and reply to users quickly.

## How it works

- Receives POST requests from LINE OA at `/webhook`
- For each text message, calls your ChatGPT API
- Replies to the user via LINE Messaging API

## Setup

1. Set environment variables:
   - `LINE_CHANNEL_ACCESS_TOKEN` (from LINE Developers Console)
   - `CHATGPT_API_KEY` (OpenAI project key)
   - `OPENAI_ASSISTANT_ID` (Assistants API ID)
   - `ADMIN_API_TOKEN` (any strong secret you will paste into the admin UI)
2. Run the server:
   ```powershell
   cd line-webhook
   go mod tidy
   go run main.go
   ```
3. Set your LINE webhook URL to `https://your-server/webhook`
4. Open the admin UI at `https://your-server/admin-ui/` (served statically by Fiber)
   - Paste the same `ADMIN_API_TOKEN` into the token box
   - Use the **Refresh** button to pull the latest pricing JSON
   - Single-field adjustments call `/admin/config/pricing/price`
   - Promotion tweaks call `/admin/config/pricing/promotion`
   - Paste + save a full JSON blob to replace `pricing_config.json`

## Dependencies

- [Fiber](https://github.com/gofiber/fiber)

## Reference

- [LINE Messaging API: Receiving messages](https://developers.line.biz/en/docs/messaging-api/receiving-messages/)
