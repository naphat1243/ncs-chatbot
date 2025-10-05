# LINE Webhook API - Individual Curl Tests
# Copy and paste these commands to test your webhook API

# ==============================================
# Test 1: Mattress Pricing Query
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_001",
        "source": {
          "userId": "test_user_001"
        },
        "message": {
          "type": "text",
          "text": "อยากทราบราคาที่นอน 6 ฟุต บริการกำจัดเชื้อโรค",
          "id": "msg_001"
        }
      }
    ]
  }'

# ==============================================
# Test 2: Sofa Pricing Query
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_002",
        "source": {
          "userId": "test_user_002"
        },
        "message": {
          "type": "text",
          "text": "โซฟา 3 ที่นั่ง ซักขจัดคราบ ราคาเท่าไหร่ครับ",
          "id": "msg_002"
        }
      }
    ]
  }'

# ==============================================
# Test 3: Package Pricing Query (Coupon)
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_003",
        "source": {
          "userId": "test_user_003"
        },
        "message": {
          "type": "text",
          "text": "แพคเพจคูปอง 5 ใบ กำจัดเชื้อโรค ราคาเท่าไหร่",
          "id": "msg_003"
        }
      }
    ]
  }'

# ==============================================
# Test 4: Curtain Pricing per Square Meter
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_004",
        "source": {
          "userId": "test_user_004"
        },
        "message": {
          "type": "text",
          "text": "ม่าน ตารางเมตรละเท่าไหร่ บริการกำจัดเชื้อโรค",
          "id": "msg_004"
        }
      }
    ]
  }'

# ==============================================
# Test 5: Member Pricing Query
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_005",
        "source": {
          "userId": "test_user_005"
        },
        "message": {
          "type": "text",
          "text": "เป็นสมาชิk NCS Family Member ที่นอน 3.5 ฟุต กำจัดเชื้อโรค ราคาเท่าไหร่",
          "id": "msg_005"
        }
      }
    ]
  }'

# ==============================================
# Test 6: Schedule/Availability Query
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_006",
        "source": {
          "userId": "test_user_006"
        },
        "message": {
          "type": "text",
          "text": "ดูตารางว่าง เดือน ตุลาคม 2568",
          "id": "msg_006"
        }
      }
    ]
  }'

# ==============================================
# Test 7: General Service Inquiry
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_007",
        "source": {
          "userId": "test_user_007"
        },
        "message": {
          "type": "text",
          "text": "มีบริการทำความสะอาดแบบไหนบ้างครับ",
          "id": "msg_007"
        }
      }
    ]
  }'

# ==============================================
# Test 8: Image Message Simulation
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_008",
        "source": {
          "userId": "test_user_008"
        },
        "message": {
          "type": "image",
          "text": "",
          "id": "test_image_001"
        }
      }
    ]
  }'

# ==============================================
# Test 9: Multiple Quick Messages (Buffer Test)
# Run these in sequence quickly to test the 15-second buffer
# ==============================================

# Message 1
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_009a",
        "source": {
          "userId": "test_user_009"
        },
        "message": {
          "type": "text",
          "text": "สวัสดีครับ",
          "id": "msg_009a"
        }
      }
    ]
  }'

# Message 2 (send within 15 seconds)
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_009b",
        "source": {
          "userId": "test_user_009"
        },
        "message": {
          "type": "text",
          "text": "อยากทราบราคาที่นอน",
          "id": "msg_009b"
        }
      }
    ]
  }'

# Message 3 (send within 15 seconds)
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_009c",
        "source": {
          "userId": "test_user_009"
        },
        "message": {
          "type": "text",
          "text": "ขนาด 6 ฟุต ค่ะ",
          "id": "msg_009c"
        }
      }
    ]
  }'

# ==============================================
# Test 10: Invalid JSON (should return 400)
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_010"
        "source": {
          "userId": "test_user_010"
        }
        "message": {
          "type": "text",
          "text": "Invalid JSON test"
        }
      }
    ]
  }'

# ==============================================
# Test 11: Empty Events Array
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": []
  }'

# ==============================================
# Test 12: Different Message Types (non-text)
# ==============================================
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_012",
        "source": {
          "userId": "test_user_012"
        },
        "message": {
          "type": "sticker",
          "packageId": "1",
          "stickerId": "1"
        }
      }
    ]
  }'

# ==============================================
# Notes:
# ==============================================
# 1. Make sure your server is running on localhost:8080
# 2. Check server logs to see the processing results
# 3. The 15-second message buffer means multiple messages from the same user 
#    within 15 seconds will be processed together
# 4. Set your environment variables:
#    - CHATGPT_API_KEY
#    - LINE_CHANNEL_ACCESS_TOKEN
#    - OPENAI_ASSISTANT_ID
# 5. Expected HTTP response codes:
#    - 200: Success
#    - 400: Bad Request (invalid JSON)
#    - 500: Internal Server Error