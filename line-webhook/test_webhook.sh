#!/bin/bash

# Test script for NCS Chatbot LINE Webhook
# Make sure the server is running on localhost:3000 before running these tests

BASE_URL="http://localhost:8080"
WEBHOOK_URL="$BASE_URL/webhook"

echo "🧪 Testing NCS Chatbot LINE Webhook"
echo "=================================="

# Test 1: Text message asking for pricing
echo "📝 Test 1: Text message - Pricing inquiry"
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_001",
        "source": {
          "userId": "test_user_123"
        },
        "message": {
          "type": "text",
          "text": "สอบถามราคาทำความสะอาดที่นอนขนาด 5 ฟุต"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n\n"

sleep 2

# Test 2: Text message asking about disinfection service
echo "📝 Test 2: Text message - Disinfection service inquiry"
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_002",
        "source": {
          "userId": "test_user_456"
        },
        "message": {
          "type": "text",
          "text": "ต้องการบริการฆ่าเชื้อโรคที่นอน 3.5 ฟุต ลูกค้าใหม่"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n\n"

sleep 2

# Test 3: Image message
echo "🖼️ Test 3: Image message"
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_003",
        "source": {
          "userId": "test_user_789"
        },
        "message": {
          "type": "image",
          "id": "test_image_123"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n\n"

sleep 2

# Test 4: Text message asking about scheduling
echo "📅 Test 4: Text message - Scheduling inquiry"
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_004",
        "source": {
          "userId": "test_user_101"
        },
        "message": {
          "type": "text",
          "text": "ต้องการจองคิว วันจันทร์หน้า เวลา 10:00 น."
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n\n"

sleep 2

# Test 5: Multiple messages from same user (should be buffered)
echo "💬 Test 5: Multiple messages from same user"
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_005a",
        "source": {
          "userId": "test_user_555"
        },
        "message": {
          "type": "text",
          "text": "สวัสดีครับ"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n"

# Send second message quickly
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_005b",
        "source": {
          "userId": "test_user_555"
        },
        "message": {
          "type": "text",
          "text": "อยากทราบราคาทำความสะอาดโซฟา"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n\n"

echo "✅ All tests completed!"
echo ""
echo "💡 Tips for testing:"
echo "1. Start the server with: go run main.go"
echo "2. Set environment variables:"
echo "   export LINE_CHANNEL_ACCESS_TOKEN=your_token"
echo "   export OPENAI_API_KEY=your_openai_key"
echo "   export OPENAI_ASSISTANT_ID=your_assistant_id"
echo "3. Check server logs for responses"
echo "4. For image testing, you need a valid LINE_CHANNEL_ACCESS_TOKEN"
