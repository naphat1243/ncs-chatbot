#!/bin/bash

# Quick API Test - Essential Tests Only
# Run this to test the most important functionality

echo "🚀 Starting Quick API Tests for LINE Webhook..."
echo "Base URL: http://localhost:8080"
echo ""

# Test 1: Basic pricing query
echo "📋 Test 1: Basic Pricing Query"
curl -s -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_001",
        "source": {
          "userId": "user_001"
        },
        "message": {
          "type": "text",
          "text": "ที่นอน 6 ฟุต กำจัดเชื้อโรค ราคาเท่าไหร่",
          "id": "msg_001"
        }
      }
    ]
  }' \
  -w "Status: %{http_code}\n"

echo ""

# Test 2: Package pricing
echo "📦 Test 2: Package Pricing Query"
curl -s -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_002",
        "source": {
          "userId": "user_002"
        },
        "message": {
          "type": "text",
          "text": "คูปอง 5 ใบ กำจัดเชื้อโรค",
          "id": "msg_002"
        }
      }
    ]
  }' \
  -w "Status: %{http_code}\n"

echo ""

# Test 3: Schedule inquiry
echo "📅 Test 3: Schedule Inquiry"
curl -s -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_003",
        "source": {
          "userId": "user_003"
        },
        "message": {
          "type": "text",
          "text": "ตารางว่าง เดือน ตุลาคม 2568",
          "id": "msg_003"
        }
      }
    ]
  }' \
  -w "Status: %{http_code}\n"

echo ""

# Test 4: Invalid JSON (should return 400)
echo "❌ Test 4: Invalid JSON (Expected 400)"
curl -s -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{"invalid": json}' \
  -w "Status: %{http_code}\n"

echo ""
echo "✅ Quick tests completed!"
echo "💡 Check server logs for detailed processing information"
echo "⏰ Remember: Messages are processed after a 15-second buffer"