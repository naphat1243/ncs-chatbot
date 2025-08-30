# NCS Chatbot LINE Webhook - Test Commands

## Prerequisites
Make sure the server is running:
```bash
cd /Users/mtb730870/WorkSpace/ncs/ncs-chatbot/line-webhook
go run main.go
```

Set environment variables:
```bash
export LINE_CHANNEL_ACCESS_TOKEN="your_line_token"
export OPENAI_API_KEY="your_openai_key"
export OPENAI_ASSISTANT_ID="your_assistant_id"
```

## Test Commands

### 1. Test Text Message - Pricing Inquiry
```bash
curl -X POST http://localhost:3000/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_001",
        "source": {
          "userId": "user_001"
        },
        "message": {
          "type": "text",
          "text": "สอบถามราคาทำความสะอาดที่นอน 5 ฟุต ลูกค้าใหม่"
        }
      }
    ]
  }'
```

### 2. Test Text Message - Disinfection Service
```bash
curl -X POST http://localhost:3000/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_002",
        "source": {
          "userId": "user_002"
        },
        "message": {
          "type": "text",
          "text": "ต้องการบริการกำจัดเชื้อโรคที่นอน 3.5ฟุต"
        }
      }
    ]
  }'
```

### 3. Test Image Message
```bash
curl -X POST http://localhost:3000/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_003",
        "source": {
          "userId": "user_003"
        },
        "message": {
          "type": "image",
          "id": "test_image_id_123"
        }
      }
    ]
  }'
```

### 4. Test Scheduling Request
```bash
curl -X POST http://localhost:3000/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_004",
        "source": {
          "userId": "user_004"
        },
        "message": {
          "type": "text",
          "text": "จองคิววันจันทร์หน้า เวลา 14:00 น. ทำความสะอาดที่นอน"
        }
      }
    ]
  }'
```

### 5. Test Pricing Function Call
```bash
curl -X POST http://localhost:3000/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_005",
        "source": {
          "userId": "user_005"
        },
        "message": {
          "type": "text",
          "text": "ขอราคาซักขจัดคราบโซฟา 2 ที่นั่ง ลูกค้าปกติ แพ็คเกจธรรมดา จำนวน 2 ตัว"
        }
      }
    ]
  }'
```

### 6. Test Package Deal Inquiry
```bash
curl -X POST http://localhost:3000/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_006",
        "source": {
          "userId": "user_006"
        },
        "message": {
          "type": "text",
          "text": "สอบถามราคาแพ็คเกจพิเศษ กำจัดเชื้อ + ซักขจัดคราบที่นอน 6ฟุต"
        }
      }
    ]
  }'
```

## Testing Multiple Messages (Message Buffering)

Send these commands quickly within 5 seconds to test message buffering:

### Message 1:
```bash
curl -X POST http://localhost:3000/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_buff1",
        "source": {
          "userId": "user_buffer_test"
        },
        "message": {
          "type": "text",
          "text": "สวัสดีครับ"
        }
      }
    ]
  }'
```

### Message 2 (send quickly after message 1):
```bash
curl -X POST http://localhost:3000/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_buff2",
        "source": {
          "userId": "user_buffer_test"
        },
        "message": {
          "type": "text",
          "text": "อยากทราบราคาทำความสะอาด"
        }
      }
    ]
  }'
```

## Expected Responses

1. **Text Messages**: Should trigger GPT assistant with pricing function calls
2. **Image Messages**: Should process image through GPT-4 Vision (requires valid LINE token)
3. **Scheduling**: Should integrate with Google Apps Script for booking
4. **Multiple Messages**: Should be buffered and processed together after 5 seconds

## Debugging Tips

1. **Check server logs** for detailed processing information
2. **Verify environment variables** are set correctly
3. **Test with valid LINE tokens** for image functionality
4. **Monitor OpenAI API usage** for function calls
5. **Check Google Apps Script integration** for scheduling requests

## Quick Test Script
```bash
# Make the test script executable and run it
chmod +x test_webhook.sh
./test_webhook.sh
```
