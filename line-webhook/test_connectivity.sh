#!/bin/bash

# Simple webhook connectivity test
# This test only checks if the webhook endpoint is responding

BASE_URL="http://localhost:8080"
WEBHOOK_URL="$BASE_URL/webhook"

echo "🔍 Testing NCS Chatbot Webhook Connectivity"
echo "==========================================="

# Test 1: Basic connectivity
echo "📡 Test 1: Basic webhook connectivity"
response=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{"events":[]}')

if [ "$response" = "200" ]; then
    echo "✅ Webhook endpoint is responding (HTTP $response)"
else
    echo "❌ Webhook endpoint not responding (HTTP $response)"
    echo "💡 Make sure to start the server with: go run main.go"
    exit 1
fi

# Test 2: Valid LINE event structure
echo ""
echo "📋 Test 2: Valid message structure"
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "connectivity_test",
        "source": {
          "userId": "test_user"
        },
        "message": {
          "type": "text",
          "text": "connectivity test"
        }
      }
    ]
  }' \
  -w "\n✅ HTTP Status: %{http_code}\n"

echo ""
echo "🎯 Quick connectivity test completed!"
echo "For full functionality testing, use: ./test_webhook.sh"
