#!/bin/bash

# LINE Webhook API Test Script
# This script tests the /webhook endpoint with various message types

# Configuration
BASE_URL="http://localhost:8080"
WEBHOOK_ENDPOINT="/webhook"
CONTENT_TYPE="application/json"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print test headers
print_test_header() {
    echo -e "\n${BLUE}===================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}===================================================${NC}\n"
}

# Function to print success message
print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

# Function to print error message
print_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Function to print info message
print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# Test 1: Simple text message about mattress pricing
print_test_header "Test 1: Text Message - Mattress Pricing Query"

curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
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
  }' \
  -w "\nHTTP Status: %{http_code}\nResponse Time: %{time_total}s\n"

if [ $? -eq 0 ]; then
    print_success "Mattress pricing query sent successfully"
else
    print_error "Failed to send mattress pricing query"
fi

# Test 2: Sofa pricing query
print_test_header "Test 2: Text Message - Sofa Pricing Query"

curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
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
  }' \
  -w "\nHTTP Status: %{http_code}\nResponse Time: %{time_total}s\n"

if [ $? -eq 0 ]; then
    print_success "Sofa pricing query sent successfully"
else
    print_error "Failed to send sofa pricing query"
fi

# Test 3: Package pricing query
print_test_header "Test 3: Text Message - Package Pricing Query"

curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
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
  }' \
  -w "\nHTTP Status: %{http_code}\nResponse Time: %{time_total}s\n"

if [ $? -eq 0 ]; then
    print_success "Package pricing query sent successfully"
else
    print_error "Failed to send package pricing query"
fi

# Test 4: Curtain/carpet pricing per square meter
print_test_header "Test 4: Text Message - Curtain Pricing Query"

curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
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
  }' \
  -w "\nHTTP Status: %{http_code}\nResponse Time: %{time_total}s\n"

if [ $? -eq 0 ]; then
    print_success "Curtain pricing query sent successfully"
else
    print_error "Failed to send curtain pricing query"
fi

# Test 5: Member pricing query
print_test_header "Test 5: Text Message - Member Pricing Query"

curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
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
          "text": "เป็นสมาชิก NCS Family Member ที่นอน 3.5 ฟุต กำจัดเชื้อโรค ราคาเท่าไหร่",
          "id": "msg_005"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\nResponse Time: %{time_total}s\n"

if [ $? -eq 0 ]; then
    print_success "Member pricing query sent successfully"
else
    print_error "Failed to send member pricing query"
fi

# Test 6: Schedule inquiry
print_test_header "Test 6: Text Message - Schedule Inquiry"

curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
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
  }' \
  -w "\nHTTP Status: %{http_code}\nResponse Time: %{time_total}s\n"

if [ $? -eq 0 ]; then
    print_success "Schedule inquiry sent successfully"
else
    print_error "Failed to send schedule inquiry"
fi

# Test 7: Image message simulation
print_test_header "Test 7: Image Message Simulation"

curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_007",
        "source": {
          "userId": "test_user_007"
        },
        "message": {
          "type": "image",
          "text": "",
          "id": "test_image_001"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\nResponse Time: %{time_total}s\n"

if [ $? -eq 0 ]; then
    print_success "Image message sent successfully"
else
    print_error "Failed to send image message"
fi

# Test 8: Multiple messages from same user (buffer test)
print_test_header "Test 8: Multiple Messages Buffer Test"

print_info "Sending first message..."
curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_008a",
        "source": {
          "userId": "test_user_008"
        },
        "message": {
          "type": "text",
          "text": "สวัสดีครับ",
          "id": "msg_008a"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n" -s

sleep 2

print_info "Sending second message..."
curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_008b",
        "source": {
          "userId": "test_user_008"
        },
        "message": {
          "type": "text",
          "text": "อยากทราบราคาที่นอน",
          "id": "msg_008b"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\n" -s

sleep 2

print_info "Sending third message..."
curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_008c",
        "source": {
          "userId": "test_user_008"
        },
        "message": {
          "type": "text",
          "text": "ขนาด 6 ฟุต ค่ะ",
          "id": "msg_008c"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\nResponse Time: %{time_total}s\n"

if [ $? -eq 0 ]; then
    print_success "Multiple messages buffer test sent successfully"
    print_info "Wait 15 seconds for message processing..."
else
    print_error "Failed to send multiple messages buffer test"
fi

# Test 9: Invalid JSON format
print_test_header "Test 9: Invalid JSON Format"

curl -X POST "${BASE_URL}${WEBHOOK_ENDPOINT}" \
  -H "Content-Type: ${CONTENT_TYPE}" \
  -d '{
    "events": [
      {
        "type": "message",
        "replyToken": "test_reply_token_009"
        "source": {
          "userId": "test_user_009"
        }
        "message": {
          "type": "text",
          "text": "Invalid JSON test"
        }
      }
    ]
  }' \
  -w "\nHTTP Status: %{http_code}\nResponse Time: %{time_total}s\n"

if [ $? -eq 0 ]; then
    print_info "Invalid JSON test completed (should return 400)"
else
    print_error "Invalid JSON test failed to send"
fi

# Test 10: Health check (non-webhook endpoint)
print_test_header "Test 10: Server Health Check"

curl -X GET "${BASE_URL}/" \
  -w "\nHTTP Status: %{http_code}\nResponse Time: %{time_total}s\n"

if [ $? -eq 0 ]; then
    print_info "Health check completed"
else
    print_error "Health check failed"
fi

print_test_header "All Tests Completed!"
print_info "Check the server logs to see the processing results."
print_info "Message buffer processing occurs after 15-second delays."

echo -e "\n${YELLOW}To monitor server logs in real-time, use:${NC}"
echo "tail -f /path/to/your/logfile"
echo -e "\n${YELLOW}Or if running with docker:${NC}"
echo "docker logs -f container_name"