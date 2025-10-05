#!/bin/bash

# Test script specifically for debugging get_available_slots_with_months function

BASE_URL="http://localhost:3000"
WEBHOOK_URL="$BASE_URL/webhook"

echo "🔍 Testing get_available_slots_with_months Function"
echo "================================================"

echo "📅 Test: Scheduling inquiry that should trigger get_available_slots_with_months"
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "debug_scheduling_001",
        "source": {
          "userId": "debug_user_schedule"
        },
        "message": {
          "type": "text",
          "text": "ขอดูวันว่างเดือนกันยายน 2567"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n\n"

sleep 3

echo "📅 Test: Another scheduling request"
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "debug_scheduling_002",
        "source": {
          "userId": "debug_user_schedule2"
        },
        "message": {
          "type": "text",
          "text": "จองคิววันจันทร์หน้า เวลา 10:00 น."
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n\n"

sleep 3

echo "📅 Test: Direct month request"
curl -X POST "$WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "debug_scheduling_003",
        "source": {
          "userId": "debug_user_schedule3"
        },
        "message": {
          "type": "text",
          "text": "ช่วยหาวันว่างให้หน่อย เดือนตุลาคม"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n\n"

echo "✅ Debugging tests completed!"
echo ""
echo "💡 Check server logs for:"
echo "1. 'get_available_slots_with_months called with arguments:'"
echo "2. 'Parsed month:'"
echo "3. 'Calling Google Apps Script:'"
echo "4. Function call status and responses"
