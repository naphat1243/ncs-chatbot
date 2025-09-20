package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// PricingConfig represents the JSON pricing configuration structure
type PricingConfig struct {
	Services      map[string]ServiceConfig      `json:"services"`
	Items         map[string]ItemConfig         `json:"items"`
	Packages      map[string]PackageConfig      `json:"packages"`
	CustomerTypes map[string]CustomerTypeConfig `json:"customer_types"`
}

type ServiceConfig struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

type ItemConfig struct {
	Name    string                `json:"name"`
	Aliases []string              `json:"aliases"`
	Sizes   map[string]SizeConfig `json:"sizes"`
}

type SizeConfig struct {
	Name    string                                       `json:"name"`
	Aliases []string                                     `json:"aliases"`
	Pricing map[string]map[string]map[string]PriceConfig `json:"pricing"` // [service][customer][package]
}

type PriceConfig struct {
	FullPrice  int `json:"full_price,omitempty"`
	Discount35 int `json:"discount_35,omitempty"`
	Discount50 int `json:"discount_50,omitempty"`
}

type PackageConfig struct {
	Name         string                  `json:"name"`
	Aliases      []string                `json:"aliases"`
	Disinfection map[string]PackagePrice `json:"disinfection,omitempty"`
	Washing      map[string]PackagePrice `json:"washing,omitempty"`
}

type PackagePrice struct {
	FullPrice  int `json:"full_price"`
	Discount   int `json:"discount"`
	SalePrice  int `json:"sale_price"`
	PerItem    int `json:"per_item"`
	DepositMin int `json:"deposit_min,omitempty"`
}

type CustomerTypeConfig struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

var pricingConfig *PricingConfig

// loadPricingConfig loads pricing configuration from JSON file
func loadPricingConfig() error {
	data, err := os.ReadFile("pricing_config.json")
	if err != nil {
		return fmt.Errorf("failed to read pricing config: %v", err)
	}

	pricingConfig = &PricingConfig{}
	if err := json.Unmarshal(data, pricingConfig); err != nil {
		return fmt.Errorf("failed to parse pricing config: %v", err)
	}

	log.Println("Pricing configuration loaded successfully")
	return nil
}

// getBangkokTime returns current time in Asia/Bangkok in RFC3339 format (YYYY-MM-DDTHH:MM:SS) without timezone suffix.
func getBangkokTime() string {
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		// Fallback to local time if loading fails
		return time.Now().Format("2006-01-02T15:04:05")
	}
	return time.Now().In(loc).Format("2006-01-02T15:04:05")
}

// extractAndProcessPricingJSON extracts JSON pricing parameters from assistant response and calls getNCSPricing
func extractAndProcessPricingJSON(response string) string {
	log.Printf("Attempting to extract JSON from response: %s", response)

	// Look for JSON pattern in the response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")

	if start == -1 || end == -1 || start >= end {
		log.Printf("No valid JSON found in response")
		return ""
	}

	jsonStr := response[start : end+1]
	log.Printf("Extracted JSON string: %s", jsonStr)

	var args struct {
		ServiceType  string `json:"service_type"`
		ItemType     string `json:"item_type"`
		Size         string `json:"size"`
		CustomerType string `json:"customer_type"`
		PackageType  string `json:"package_type"`
		Quantity     int    `json:"quantity"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &args); err != nil {
		log.Printf("Failed to parse extracted JSON: %v", err)
		return ""
	}

	log.Printf("Successfully parsed JSON: ServiceType=%s, ItemType=%s, Size=%s, CustomerType=%s, PackageType=%s, Quantity=%d",
		args.ServiceType, args.ItemType, args.Size, args.CustomerType, args.PackageType, args.Quantity)

	// Call the pricing function with the extracted parameters
	result := getNCSPricing(args.ServiceType, args.ItemType, args.Size, args.CustomerType, args.PackageType, args.Quantity)
	log.Printf("Pricing function result: %s", result)

	return result
}

type LineEvent struct {
	Events []struct {
		Type       string `json:"type"`
		ReplyToken string `json:"replyToken"`
		Source     struct {
			UserID string `json:"userId"`
		} `json:"source"`
		Message struct {
			Type string `json:"type"`
			Text string `json:"text"`
			ID   string `json:"id"`
		} `json:"message"`
	} `json:"events"`
}

var (
	userThreadMap  = make(map[string]string)
	userThreadLock sync.Mutex

	userLastQAMap = make(map[string]struct {
		Question string
		Answer   string
	})

	userMsgBuffer = make(map[string][]string) // buffer for each user
	userMsgTimer  = make(map[string]*time.Timer)
)

func main() {
	// Load pricing configuration
	if err := loadPricingConfig(); err != nil {
		log.Fatal("Failed to load pricing configuration:", err)
	}

	app := fiber.New()

	app.Post("/webhook", func(c *fiber.Ctx) error {
		var event LineEvent
		if err := json.Unmarshal(c.Body(), &event); err != nil {
			return c.SendStatus(fiber.StatusBadRequest)
		}
		for _, e := range event.Events {
			if e.Type == "message" {
				userId := e.Source.UserID
				var messageContent string

				if e.Message.Type == "text" {
					messageContent = e.Message.Text
				} else if e.Message.Type == "image" {
					// Handle image message
					imageURL, err := getLineImageURL(e.Message.ID)
					if err != nil {
						log.Println("Error getting image URL:", err)
						messageContent = "ได้รับรูปภาพจากลูกค้า (ไม่สามารถแสดงได้)"
					} else {
						messageContent = "ลูกค้าส่งรูปภาพ: " + imageURL
					}
				} else {
					// Skip other message types
					continue
				}

				userThreadLock.Lock()
				userMsgBuffer[userId] = append(userMsgBuffer[userId], messageContent)

				// Stop existing timer if any
				if timer, ok := userMsgTimer[userId]; ok {
					timer.Stop()
				}

				// Capture replyToken to avoid closure issues
				replyToken := e.ReplyToken

				// Set new timer for 15 seconds
				t := time.AfterFunc(15*time.Second, func() {
					userThreadLock.Lock()
					msgs := userMsgBuffer[userId]
					userMsgBuffer[userId] = nil
					delete(userMsgTimer, userId) // Clean up timer reference
					userThreadLock.Unlock()

					if len(msgs) == 0 {
						log.Printf("No messages to process for user %s", userId)
						return
					}

					var summary string
					if len(msgs) == 1 {
						summary = msgs[0]
						log.Printf("Single message from user %s: %s", userId, summary)
					} else {
						summary = fmt.Sprintf("สรุปคำถาม %d ข้อความจากลูกค้า: %v", len(msgs), msgs)
						log.Printf("Multiple messages (%d) from user %s: %v", len(msgs), userId, msgs)
					}

					responseText := getAssistantResponse(userId, summary)
					replyToLine(replyToken, responseText)
				})

				userMsgTimer[userId] = t
				userThreadLock.Unlock()

				log.Printf("Message buffered for user %s (total: %d messages). Timer set for 15 seconds.", userId, len(userMsgBuffer[userId]))
			}
		}
		return c.SendStatus(fiber.StatusOK)
	})

	log.Fatal(app.Listen(":8080"))
}

// getLineImageURL gets the image URL from LINE and converts it to a base64 data URL for GPT vision
func getLineImageURL(messageID string) (string, error) {
	channelToken := os.Getenv("LINE_CHANNEL_ACCESS_TOKEN")
	if channelToken == "" {
		return "", fmt.Errorf("LINE channel access token not set")
	}

	// Get image content from LINE
	imageURL := "https://api-data.line.me/v2/bot/message/" + messageID + "/content"
	req, err := http.NewRequest("GET", imageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+channelToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to get image: %s", resp.Status)
	}

	// Read image data
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading image data: %v", err)
	}

	// Get content type or default to image/jpeg
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	// Convert to base64 data URL for GPT-4 Vision
	base64Data := base64.StdEncoding.EncodeToString(imageData)
	return fmt.Sprintf("data:%s;base64,%s", contentType, base64Data), nil
}

// isErrorResponse checks if a response is an error message that shouldn't be cached
func isErrorResponse(response string) bool {
	errorKeywords := []string{
		"Error ",
		"Failed to ",
		"not configured",
		"not set",
		"Error creating",
		"Error running",
		"Error sending",
		"Error getting",
		"Error calling",
		"ขออภัย ระบบมีปัญหา", // Our user-friendly error messages
		"เกิดข้อผิดพลาด",
		"ไม่สามารถ",    // Unable to
		"พบข้อผิดพลาด", // Found error
	}

	for _, keyword := range errorKeywords {
		if strings.Contains(response, keyword) {
			return true
		}
	}

	// Also check if response is empty or too short to be useful
	if len(strings.TrimSpace(response)) < 10 {
		return true
	}

	return false
} // getAssistantResponse uses OpenAI Assistants API, mapping userId to threadId in-memory
func getAssistantResponse(userId, message string) string {
	log.Printf("getAssistantResponse called for user %s with message: %s", userId, message)

	// Check for duplicate question - return previous answer to save costs
	userThreadLock.Lock()
	lastQA, hasLast := userLastQAMap[userId]
	userThreadLock.Unlock()
	if hasLast && lastQA.Question == message && lastQA.Answer != "" {
		// Only return cached answer if it's not an error message
		if !isErrorResponse(lastQA.Answer) {
			log.Printf("Duplicate question detected for user %s, returning cached answer", userId)
			return lastQA.Answer
		} else {
			log.Printf("Cached response is an error, will generate new response for user %s", userId)
		}
	}

	apiKey := os.Getenv("CHATGPT_API_KEY")
	if apiKey == "" {
		log.Printf("OpenAI API key not configured for user %s", userId)
		return "ขออภัย ระบบมีปัญหาชั่วคราว กรุณาลองใหม่อีกครั้งหรือติดต่อเจ้าหน้าที่"
	}
	client := &http.Client{}

	userThreadLock.Lock()
	threadId, ok := userThreadMap[userId]
	userThreadLock.Unlock()

	if !ok {
		// Create new thread
		threadReq := map[string]interface{}{}
		threadPayload, _ := json.Marshal(threadReq)
		req, _ := http.NewRequest("POST", "https://api.openai.com/v1/threads", bytes.NewReader(threadPayload))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("OpenAI-Beta", "assistants=v2")
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error creating thread for user %s: %v", userId, err)
			return "ขออภัย ระบบมีปัญหาชั่วคราว กรุณาลองใหม่อีกครั้ง"
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var threadResp struct {
			ID string `json:"id"`
		}
		json.Unmarshal(body, &threadResp)
		threadId = threadResp.ID
		if threadId == "" {
			log.Printf("Failed to create thread. Status: %v, Body: %s", resp.Status, string(body))
			return "Failed to create thread."
		}
		userThreadLock.Lock()
		userThreadMap[userId] = threadId
		userThreadLock.Unlock()
	}

	// Get current time in Asia/Bangkok (local calculation – no external API dependency)
	var timeStr = getBangkokTime()

	// Add message to thread (with current time for GPT)
	var msgReq map[string]interface{}

	// Check if message contains image URL
	if len(message) > 20 && (message[:15] == "ลูกค้าส่งรูปภาพ: " || message[:15] == "ลูกค้าส่งรูปภาพ:") {
		// Handle image message with vision
		imageURL := message[15:] // Extract URL after "ลูกค้าส่งรูปภาพ: "
		msgReq = map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("ขณะนี้เวลา %s: ลูกค้าส่งรูปภาพมา กรุณาวิเคราะห์รูปภาพและให้คำแนะนำเกี่ยวกับบริการทำความสะอาดที่เหมาะสม", timeStr),
				},
				{
					"type": "image_url",
					"image_url": map[string]string{
						"url": imageURL,
					},
				},
			},
		}
	} else {
		// Handle text message
		msgReq = map[string]interface{}{
			"role":    "user",
			"content": fmt.Sprintf("ขณะนี้เวลา %s: %s", timeStr, message),
		}
	}

	msgPayload, _ := json.Marshal(msgReq)
	msgUrl := "https://api.openai.com/v1/threads/" + threadId + "/messages"
	msgReqHttp, _ := http.NewRequest("POST", msgUrl, bytes.NewReader(msgPayload))
	msgReqHttp.Header.Set("Authorization", "Bearer "+apiKey)
	msgReqHttp.Header.Set("Content-Type", "application/json")
	msgReqHttp.Header.Set("OpenAI-Beta", "assistants=v2")
	msgResp, err := client.Do(msgReqHttp)
	if err != nil {
		return "Error sending message to thread."
	}
	defer msgResp.Body.Close()
	body, _ := io.ReadAll(msgResp.Body)
	var msgRespObj map[string]interface{}
	json.Unmarshal(body, &msgRespObj)

	// Run the assistant
	assistantId := os.Getenv("OPENAI_ASSISTANT_ID")
	if assistantId == "" {
		log.Printf("OPENAI_ASSISTANT_ID not set")
		return "OPENAI_ASSISTANT_ID not set."
	}

	log.Printf("Running assistant %s on thread %s", assistantId, threadId)

	// Check for active runs first and cancel them if needed
	listRunsUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs"
	listRunsReq, _ := http.NewRequest("GET", listRunsUrl, nil)
	listRunsReq.Header.Set("Authorization", "Bearer "+apiKey)
	listRunsReq.Header.Set("OpenAI-Beta", "assistants=v2")
	listRunsResp, err := client.Do(listRunsReq)
	if err == nil {
		defer listRunsResp.Body.Close()
		listRunsBody, _ := io.ReadAll(listRunsResp.Body)
		var listRunsObj struct {
			Data []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"data"`
		}
		json.Unmarshal(listRunsBody, &listRunsObj)

		// Cancel any active runs
		for _, run := range listRunsObj.Data {
			if run.Status == "in_progress" || run.Status == "requires_action" {
				log.Printf("Found active run %s with status %s, cancelling it", run.ID, run.Status)
				cancelUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs/" + run.ID + "/cancel"
				cancelReq, _ := http.NewRequest("POST", cancelUrl, nil)
				cancelReq.Header.Set("Authorization", "Bearer "+apiKey)
				cancelReq.Header.Set("OpenAI-Beta", "assistants=v2")
				cancelResp, err := client.Do(cancelReq)
				if err == nil {
					defer cancelResp.Body.Close()
					log.Printf("Cancelled run %s", run.ID)
				} else {
					log.Printf("Failed to cancel run %s: %v", run.ID, err)
				}
			}
		}
	}

	runReq := map[string]interface{}{
		"assistant_id": assistantId,
	}
	runPayload, _ := json.Marshal(runReq)
	runUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs"
	runReqHttp, _ := http.NewRequest("POST", runUrl, bytes.NewReader(runPayload))
	runReqHttp.Header.Set("Authorization", "Bearer "+apiKey)
	runReqHttp.Header.Set("Content-Type", "application/json")
	runReqHttp.Header.Set("OpenAI-Beta", "assistants=v2")
	runResp, err := client.Do(runReqHttp)
	if err != nil {
		log.Printf("Error running assistant: %v", err)
		return "Error running assistant."
	}
	defer runResp.Body.Close()
	body, _ = io.ReadAll(runResp.Body)

	log.Printf("Assistant run response: %s", string(body))

	var runRespObj struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	json.Unmarshal(body, &runRespObj)

	// If there's an error about active run, try to handle it
	if runRespObj.Error.Type == "invalid_request_error" && runRespObj.ID == "" {
		log.Printf("Run creation failed with error: %s", runRespObj.Error.Message)

		// Try to extract run ID from error message and cancel it
		if strings.Contains(runRespObj.Error.Message, "already has an active run") {
			// Extract run ID from error message like "run_O1YyJLu1c08K603vr1kelKJb"
			words := strings.Fields(runRespObj.Error.Message)
			for _, word := range words {
				if strings.HasPrefix(word, "run_") {
					runId := strings.TrimSuffix(word, ".")
					log.Printf("Attempting to cancel active run: %s", runId)

					cancelUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs/" + runId + "/cancel"
					cancelReq, _ := http.NewRequest("POST", cancelUrl, nil)
					cancelReq.Header.Set("Authorization", "Bearer "+apiKey)
					cancelReq.Header.Set("OpenAI-Beta", "assistants=v2")
					cancelResp, err := client.Do(cancelReq)
					if err == nil {
						defer cancelResp.Body.Close()
						log.Printf("Successfully cancelled run %s", runId)

						// Wait a moment and try creating the run again
						time.Sleep(2 * time.Second)

						// Retry creating the run
						runResp2, err := client.Do(runReqHttp)
						if err == nil {
							defer runResp2.Body.Close()
							body2, _ := io.ReadAll(runResp2.Body)
							log.Printf("Retry run response: %s", string(body2))
							json.Unmarshal(body2, &runRespObj)
						}
					}
					break
				}
			}
		}
	}

	if runRespObj.ID == "" {
		log.Printf("Failed to start run. Response: %s", string(body))
		return "Failed to start run."
	}

	log.Printf("Assistant run started with ID: %s, initial status: %s", runRespObj.ID, runRespObj.Status)

	// Poll run status and get response waiting 60 sec
	var lastToolCallSignature string
	var submittedToolOutputs bool
	for i := 0; i < 60; i++ {
		runStatusUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs/" + runRespObj.ID
		runStatusReq, _ := http.NewRequest("GET", runStatusUrl, nil)
		runStatusReq.Header.Set("Authorization", "Bearer "+apiKey)
		runStatusReq.Header.Set("OpenAI-Beta", "assistants=v2")
		runStatusResp, err := client.Do(runStatusReq)
		if err != nil {
			return "Error polling run status."
		}
		statusBody, _ := io.ReadAll(runStatusResp.Body)
		runStatusResp.Body.Close()
		var statusObj struct {
			Status         string `json:"status"`
			RequiredAction struct {
				Type              string `json:"type"`
				SubmitToolOutputs struct {
					ToolCalls []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"submit_tool_outputs"`
			} `json:"required_action"`
		}
		json.Unmarshal(statusBody, &statusObj)
		log.Printf("Run status: %s", statusObj.Status)

		// Add detailed logging for function calls
		if statusObj.RequiredAction.Type == "submit_tool_outputs" {
			log.Printf("Function calls required: %d", len(statusObj.RequiredAction.SubmitToolOutputs.ToolCalls))
		}

		// --- เช็ค required_action.submit_tool_outputs.tool_calls ใน /runs ---
		if statusObj.RequiredAction.Type == "submit_tool_outputs" && len(statusObj.RequiredAction.SubmitToolOutputs.ToolCalls) > 0 {
			// Build a signature of current tool call IDs to detect duplicates
			var ids []string
			for _, c := range statusObj.RequiredAction.SubmitToolOutputs.ToolCalls {
				ids = append(ids, c.ID)
			}
			currentSignature := strings.Join(ids, ",")
			if currentSignature == lastToolCallSignature && submittedToolOutputs {
				// Already submitted these tool outputs; wait for assistant to process
				log.Printf("Tool outputs already submitted for signature %s; waiting...", currentSignature)
				time.Sleep(800 * time.Millisecond)
				continue
			}
			var aggregatedOutputs []map[string]interface{}
			for _, call := range statusObj.RequiredAction.SubmitToolOutputs.ToolCalls {
				log.Printf("Processing function call: %s", call.Function.Name)

				if call.Function.Name == "get_available_slots_with_months" {
					log.Printf("get_available_slots_with_months called with arguments: %s", string(call.Function.Arguments))
					var args struct {
						ThaiMonthYear string `json:"thai_month_year"`
					}
					if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
						var argStr string
						json.Unmarshal(call.Function.Arguments, &argStr)
						json.Unmarshal([]byte(argStr), &args)
					}
					if args.ThaiMonthYear != "" {
						gsUrl := "https://script.google.com/macros/s/AKfycbwfSkwsgO56UdPHqa-KCxO7N-UDzkiMIBVjBTd0k8sowLtm7wORC-lN32IjAwtOVqMxQw/exec?sheet=" + url.QueryEscape(args.ThaiMonthYear)
						resp, err := http.Get(gsUrl)
						if err != nil {
							aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error calling Google Apps Script."})
						} else {
							bodySlots, _ := io.ReadAll(resp.Body)
							resp.Body.Close()
							aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": string(bodySlots)})
						}
					} else {
						aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "ไม่พบเดือน"})
					}
				} else if call.Function.Name == "get_ncs_pricing" {
					log.Printf("get_ncs_pricing called with arguments: %s", string(call.Function.Arguments))
					var args struct {
						ServiceType  string `json:"service_type"`
						ItemType     string `json:"item_type"`
						Size         string `json:"size,omitempty"`
						CustomerType string `json:"customer_type,omitempty"`
						PackageType  string `json:"package_type,omitempty"`
						Quantity     int    `json:"quantity,omitempty"`
					}

					// Try direct unmarshaling first
					if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
						// If that fails, try double unmarshaling (string wrapped)
						var argStr string
						if err2 := json.Unmarshal(call.Function.Arguments, &argStr); err2 == nil {
							if err3 := json.Unmarshal([]byte(argStr), &args); err3 != nil {
								log.Printf("Failed to parse get_ncs_pricing arguments after double unmarshal: %v", err3)
								aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error parsing pricing arguments: " + err3.Error()})
								continue
							}
						} else {
							log.Printf("Failed to parse get_ncs_pricing arguments: %v", err)
							aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error parsing pricing arguments: " + err.Error()})
							continue
						}
					}

					// Set defaults for optional parameters according to GPT function definition
					if args.CustomerType == "" {
						args.CustomerType = "new" // Default to new customer
					}
					if args.PackageType == "" {
						args.PackageType = "regular" // Default to regular pricing
					}
					if args.Quantity == 0 {
						args.Quantity = 1 // Default quantity
					}

					log.Printf("Parsed pricing arguments: ServiceType='%s', ItemType='%s', Size='%s', CustomerType='%s', PackageType='%s', Quantity=%d",
						args.ServiceType, args.ItemType, args.Size, args.CustomerType, args.PackageType, args.Quantity)

					result := getNCSPricing(args.ServiceType, args.ItemType, args.Size, args.CustomerType, args.PackageType, args.Quantity)
					log.Printf("Pricing function result: %s", result)
					aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": result})
				}
			}
			if len(aggregatedOutputs) > 0 {
				payload, _ := json.Marshal(map[string]interface{}{"tool_outputs": aggregatedOutputs})
				submitUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs/" + runRespObj.ID + "/submit_tool_outputs"
				submitReq, _ := http.NewRequest("POST", submitUrl, bytes.NewReader(payload))
				submitReq.Header.Set("Authorization", "Bearer "+apiKey)
				submitReq.Header.Set("Content-Type", "application/json")
				submitReq.Header.Set("OpenAI-Beta", "assistants=v2")
				resp, err := client.Do(submitReq)
				if err != nil {
					log.Printf("Error submitting aggregated tool outputs: %v", err)
				} else {
					bodySubmit, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					log.Printf("Submitted %d tool outputs. Status: %d Body: %s", len(aggregatedOutputs), resp.StatusCode, string(bodySubmit))
				}
				lastToolCallSignature = currentSignature
				submittedToolOutputs = true
				// Small delay to allow run state update
				time.Sleep(700 * time.Millisecond)
				continue
			}
		}
		// Reset flag if run moved past requires_action
		if statusObj.Status != "requires_action" {
			submittedToolOutputs = false
		}
		if statusObj.Status == "completed" {
			break
		}
	}

	// Get messages (last assistant message)
	getMsgUrl := "https://api.openai.com/v1/threads/" + threadId + "/messages"
	getMsgReq, _ := http.NewRequest("GET", getMsgUrl, nil)
	getMsgReq.Header.Set("Authorization", "Bearer "+apiKey)
	getMsgReq.Header.Set("OpenAI-Beta", "assistants=v2")
	getMsgResp, err := client.Do(getMsgReq)
	if err != nil {
		return "Error getting messages."
	}
	defer getMsgResp.Body.Close()
	body, _ = io.ReadAll(getMsgResp.Body)
	var msgList struct {
		Data []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text struct {
					Value string `json:"value"`
				} `json:"text"`
			} `json:"content"`
		} `json:"data"`
	}
	json.Unmarshal(body, &msgList)
	for i := 0; i < len(msgList.Data); i++ {
		if msgList.Data[i].Role == "assistant" && len(msgList.Data[i].Content) > 0 {
			if msgList.Data[i].Content[0].Type == "text" {
				reply := msgList.Data[i].Content[0].Text.Value
				log.Printf("Assistant text response: %s", reply)

				// Check if the response contains JSON pricing parameters (GPT returning JSON instead of calling function)
				if strings.Contains(reply, "service_type") && strings.Contains(reply, "item_type") {
					log.Printf("Detected JSON pricing parameters in text response, attempting to parse and call function")
					// Try to extract and parse JSON from the response
					if pricingResult := extractAndProcessPricingJSON(reply); pricingResult != "" {
						log.Printf("Successfully processed pricing JSON: %s", pricingResult)
						return pricingResult
					}
				}

				if reply != "" && !isErrorResponse(reply) {
					// Only store successful responses, not error messages
					userThreadLock.Lock()
					userLastQAMap[userId] = struct {
						Question string
						Answer   string
					}{Question: message, Answer: reply}
					userThreadLock.Unlock()
					log.Printf("Cached successful response for user %s", userId)
					fmt.Println(reply)
					return reply
				} else if reply != "" {
					// Return error response but don't cache it
					log.Printf("Not caching error response for user %s", userId)
					fmt.Println(reply)
					return reply
				}
			}
			// --- handle function call/tool_calls ---
			if msgList.Data[i].Content[0].Type == "tool_calls" {
				var toolCalls []struct {
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				}
				_ = json.Unmarshal([]byte(msgList.Data[i].Content[0].Text.Value), &toolCalls)
				for _, call := range toolCalls {
					if call.Function.Name == "get_available_slots_with_months" {
						// Unmarshal 2 ชั้น
						var argStr string
						_ = json.Unmarshal(call.Function.Arguments, &argStr)
						var args struct {
							ThaiMonthYear string `json:"thai_month_year"`
						}
						_ = json.Unmarshal([]byte(argStr), &args)
						fmt.Println("get_available_slots_with_months has been called")
						fmt.Printf("Parsed arguments for get_available_slots_with_months: %+v\n", args)
						if args.ThaiMonthYear != "" {
							fmt.Printf("Calling Google Apps Script for month: %s\n", args.ThaiMonthYear)
							month := args.ThaiMonthYear
							// Call Google Apps Script
							url := "https://script.google.com/macros/s/AKfycbwfSkwsgO56UdPHqa-KCxO7N-UDzkiMIBVjBTd0k8sowLtm7wORC-lN32IjAwtOVqMxQw/exec?sheet=" + month
							resp, err := http.Get(url)
							if err != nil {
								return "Error calling Google Apps Script."
							}
							defer resp.Body.Close()
							gsBody, _ := io.ReadAll(resp.Body)
							result := string(gsBody)

							// ส่งข้อมูลวันว่างกลับไปให้ GPT เพื่อสรุปให้ลูกค้า
							msgReq := map[string]interface{}{
								"role":    "user",
								"content": fmt.Sprintf("วันว่างที่ได้จากระบบ: %s ช่วยสรุปให้ลูกค้าแบบสวยงาม", result),
							}
							msgPayload, _ := json.Marshal(msgReq)
							msgUrl := "https://api.openai.com/v1/threads/" + threadId + "/messages"
							msgReqHttp, _ := http.NewRequest("POST", msgUrl, bytes.NewReader(msgPayload))
							msgReqHttp.Header.Set("Authorization", "Bearer "+apiKey)
							msgReqHttp.Header.Set("Content-Type", "application/json")
							msgReqHttp.Header.Set("OpenAI-Beta", "assistants=v2")
							msgResp, err := client.Do(msgReqHttp)
							if err != nil {
								return "Error sending slot info to GPT."
							}
							defer msgResp.Body.Close()
							_, _ = io.ReadAll(msgResp.Body)

							// Run assistant อีกรอบ
							runReq := map[string]interface{}{
								"assistant_id": assistantId,
							}
							runPayload, _ := json.Marshal(runReq)
							runUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs"
							runReqHttp, _ := http.NewRequest("POST", runUrl, bytes.NewReader(runPayload))
							runReqHttp.Header.Set("Authorization", "Bearer "+apiKey)
							runReqHttp.Header.Set("Content-Type", "application/json")
							runReqHttp.Header.Set("OpenAI-Beta", "assistants=v2")
							runResp, err := client.Do(runReqHttp)
							if err != nil {
								return "Error running assistant for slot summary."
							}
							defer runResp.Body.Close()
							_, _ = io.ReadAll(runResp.Body)

							// Poll run status
							for j := 0; j < 20; j++ {
								runStatusUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs/" + runRespObj.ID
								runStatusReq, _ := http.NewRequest("GET", runStatusUrl, nil)
								runStatusReq.Header.Set("Authorization", "Bearer "+apiKey)
								runStatusReq.Header.Set("OpenAI-Beta", "assistants=v2")
								runStatusResp, err := client.Do(runStatusReq)
								if err != nil {
									return "Error polling run status for slot summary."
								}
								defer runStatusResp.Body.Close()
								statusBody, _ := io.ReadAll(runStatusResp.Body)
								var statusObj2 struct {
									Status string `json:"status"`
								}
								json.Unmarshal(statusBody, &statusObj2)
								if statusObj2.Status == "completed" {
									break
								}
							}

							// Get messages (last assistant message)
							getMsgUrl := "https://api.openai.com/v1/threads/" + threadId + "/messages"
							getMsgReq, _ := http.NewRequest("GET", getMsgUrl, nil)
							getMsgReq.Header.Set("Authorization", "Bearer "+apiKey)
							getMsgReq.Header.Set("OpenAI-Beta", "assistants=v2")
							getMsgResp, err := client.Do(getMsgReq)
							if err != nil {
								return "Error getting slot summary from GPT."
							}
							defer getMsgResp.Body.Close()
							body, _ := io.ReadAll(getMsgResp.Body)
							var slotMsgList struct {
								Data []struct {
									Role    string `json:"role"`
									Content []struct {
										Type string `json:"type"`
										Text struct {
											Value string `json:"value"`
										} `json:"text"`
									} `json:"content"`
								} `json:"data"`
							}
							json.Unmarshal(body, &slotMsgList)
							for k := len(slotMsgList.Data) - 1; k >= 0; k-- {
								if slotMsgList.Data[k].Role == "assistant" && len(slotMsgList.Data[k].Content) > 0 {
									if slotMsgList.Data[k].Content[0].Type == "text" {
										reply := slotMsgList.Data[k].Content[0].Text.Value
										if reply != "" {
											return reply
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return ""
}

// Helper functions for JSON-based pricing
func normalizeAlias(input string, aliases []string) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	for _, alias := range aliases {
		if strings.ToLower(alias) == input {
			return true
		}
	}
	return false
}

func findServiceKey(input string) string {
	for key, service := range pricingConfig.Services {
		if normalizeAlias(input, service.Aliases) {
			return key
		}
	}
	return ""
}

func findItemKey(input string) string {
	for key, item := range pricingConfig.Items {
		if normalizeAlias(input, item.Aliases) {
			return key
		}
	}
	return ""
}

func findPackageKey(input string) string {
	for key, pkg := range pricingConfig.Packages {
		if normalizeAlias(input, pkg.Aliases) {
			return key
		}
	}
	return ""
}

func findCustomerKey(input string) string {
	for key, customer := range pricingConfig.CustomerTypes {
		if normalizeAlias(input, customer.Aliases) {
			return key
		}
	}
	return ""
}

func findSizeKey(input string, sizes map[string]SizeConfig) string {
	for key, size := range sizes {
		if normalizeAlias(input, size.Aliases) {
			return key
		}
	}
	return ""
}

func formatPrice(price PriceConfig, serviceName, itemName, sizeName, customerName string) string {
	var result strings.Builder

	result.WriteString(fmt.Sprintf("%s %s บริการ%s", itemName, sizeName, serviceName))

	if customerName != "" {
		result.WriteString(fmt.Sprintf(" สำหรับ%s", customerName))
	}
	result.WriteString(": ")

	parts := []string{}
	if price.FullPrice > 0 {
		parts = append(parts, fmt.Sprintf("ราคาเต็ม %s บาท", formatNumber(price.FullPrice)))
	}
	if price.Discount35 > 0 {
		parts = append(parts, fmt.Sprintf("ลด 35%% = %s บาท", formatNumber(price.Discount35)))
	}
	if price.Discount50 > 0 {
		parts = append(parts, fmt.Sprintf("ลด 50%% = %s บาท", formatNumber(price.Discount50)))
	}

	result.WriteString(strings.Join(parts, ", "))
	return result.String()
}

func formatPackagePrice(pkg PackagePrice, serviceName, packageName string, quantity int) string {
	depositInfo := ""
	if pkg.DepositMin > 0 {
		depositInfo = fmt.Sprintf(" มัดจำขั้นต่ำ %s บาท", formatNumber(pkg.DepositMin))
	}

	return fmt.Sprintf("%s %d ใบ บริการ%s: ราคาเต็ม %s บาท, ส่วนลด %s บาท, ราคาขาย %s บาท (เฉลี่ย %s บาท/ใบ)%s",
		packageName, quantity, serviceName,
		formatNumber(pkg.FullPrice),
		formatNumber(pkg.Discount),
		formatNumber(pkg.SalePrice),
		formatNumber(pkg.PerItem),
		depositInfo)
}

func formatNumber(n int) string {
	str := fmt.Sprintf("%d", n)
	if len(str) <= 3 {
		return str
	}

	var result strings.Builder
	for i, r := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result.WriteString(",")
		}
		result.WriteRune(r)
	}
	return result.String()
}

// getNCSPricingJSON returns pricing information using JSON configuration
func getNCSPricingJSON(serviceType, itemType, size, customerType, packageType string, quantity int) string {
	if pricingConfig == nil {
		return "ระบบราคายังไม่พร้อมใช้งาน กรุณาลองใหม่อีกครั้ง"
	}

	log.Printf("getNCSPricingJSON called with: serviceType='%s', itemType='%s', size='%s', customerType='%s', packageType='%s', quantity=%d",
		serviceType, itemType, size, customerType, packageType, quantity)

	// Normalize inputs
	serviceKey := findServiceKey(serviceType)
	itemKey := findItemKey(itemType)
	customerKey := findCustomerKey(customerType)
	packageKey := findPackageKey(packageType)

	// Set defaults
	if customerKey == "" {
		customerKey = "new" // default customer type
	}
	if packageKey == "" {
		packageKey = "regular" // default package type
	}

	log.Printf("Normalized keys: serviceKey='%s', itemKey='%s', customerKey='%s', packageKey='%s'",
		serviceKey, itemKey, customerKey, packageKey)

	// Handle package pricing
	if packageKey != "regular" {
		return handlePackagePricing(serviceKey, packageKey, quantity)
	}

	// Handle regular item pricing
	if serviceKey == "" || itemKey == "" {
		return generateFallbackResponse(serviceType, itemType, size)
	}

	return handleItemPricing(serviceKey, itemKey, size, customerKey)
}

func handlePackagePricing(serviceKey, packageKey string, quantity int) string {
	pkg, exists := pricingConfig.Packages[packageKey]
	if !exists {
		return "ไม่พบข้อมูลแพคเพจที่ระบุ"
	}

	serviceName := ""
	if serviceKey != "" {
		if svc, exists := pricingConfig.Services[serviceKey]; exists {
			serviceName = svc.Name
		}
	} else {
		serviceName = "ทำความสะอาด"
	}

	quantityStr := fmt.Sprintf("%d", quantity)

	if serviceKey == "disinfection" && pkg.Disinfection != nil {
		if price, exists := pkg.Disinfection[quantityStr]; exists {
			return formatPackagePrice(price, serviceName, pkg.Name, quantity)
		}
	} else if serviceKey == "washing" && pkg.Washing != nil {
		if price, exists := pkg.Washing[quantityStr]; exists {
			return formatPackagePrice(price, serviceName, pkg.Name, quantity)
		}
	}

	return fmt.Sprintf("ไม่พบข้อมูลราคา%s %d ใบ สำหรับบริการ%s", pkg.Name, quantity, serviceName)
}

func handleItemPricing(serviceKey, itemKey, size, customerKey string) string {
	item, exists := pricingConfig.Items[itemKey]
	if !exists {
		return "ไม่พบข้อมูลสินค้าที่ระบุ"
	}

	service := pricingConfig.Services[serviceKey]
	customer := pricingConfig.CustomerTypes[customerKey]

	// Handle case where no size is specified
	if size == "" {
		return generateItemSizeList(serviceKey, itemKey, customerKey)
	}

	// Find size
	sizeKey := findSizeKey(size, item.Sizes)
	if sizeKey == "" {
		return generateItemSizeList(serviceKey, itemKey, customerKey)
	}

	sizeConfig := item.Sizes[sizeKey]

	// Get pricing
	if servicePricing, exists := sizeConfig.Pricing[serviceKey]; exists {
		if customerPricing, exists := servicePricing[customerKey]; exists {
			if regularPricing, exists := customerPricing["regular"]; exists {
				return formatPrice(regularPricing, service.Name, item.Name, sizeConfig.Name, customer.Name)
			}
		}
	}

	return fmt.Sprintf("ไม่พบข้อมูลราคา%s %s %s สำหรับ%s", item.Name, sizeConfig.Name, service.Name, customer.Name)
}

func generateItemSizeList(serviceKey, itemKey, customerKey string) string {
	item := pricingConfig.Items[itemKey]
	service := pricingConfig.Services[serviceKey]
	customer := pricingConfig.CustomerTypes[customerKey]

	var result strings.Builder
	result.WriteString(fmt.Sprintf("บริการทำความสะอาด%s %s", item.Name, service.Name))
	if customerKey != "new" {
		result.WriteString(fmt.Sprintf(" สำหรับ%s", customer.Name))
	}
	result.WriteString(":\n")

	count := 0
	for _, sizeConfig := range item.Sizes {
		if servicePricing, exists := sizeConfig.Pricing[serviceKey]; exists {
			if customerPricing, exists := servicePricing[customerKey]; exists {
				if pricing, exists := customerPricing["regular"]; exists {
					count++
					result.WriteString(fmt.Sprintf("• %s %s: ", item.Name, sizeConfig.Name))

					parts := []string{}
					if pricing.FullPrice > 0 {
						parts = append(parts, fmt.Sprintf("%s บาท", formatNumber(pricing.FullPrice)))
					}
					if pricing.Discount35 > 0 {
						parts = append(parts, fmt.Sprintf("ลด 35%% = %s บาท", formatNumber(pricing.Discount35)))
					}
					if pricing.Discount50 > 0 {
						parts = append(parts, fmt.Sprintf("ลด 50%% = %s บาท", formatNumber(pricing.Discount50)))
					}
					result.WriteString(strings.Join(parts, ", "))
					result.WriteString("\n")
				}
			}
		}
	}

	if count == 0 {
		return fmt.Sprintf("ไม่พบข้อมูลราคา%s สำหรับบริการ%s", item.Name, service.Name)
	}

	result.WriteString(fmt.Sprintf("\nกรุณาระบุขนาด%sเพื่อข้อมูลราคาที่แม่นยำ", item.Name))
	return result.String()
}

func generateFallbackResponse(serviceType, itemType, size string) string {
	return fmt.Sprintf("ขออภัย ไม่พบข้อมูลราคาสำหรับ บริการ: '%s' สินค้า: '%s' ขนาด: '%s'\n\nกรุณาติดต่อเจ้าหน้าที่เพื่อสอบถามราคาเพิ่มเติม หรือระบุรายละเอียดให้ชัดเจนมากขึ้น เช่น:\n• ประเภทบริการ (กำจัดเชื้อโรค หรือ ซักขจัดคราบ)\n• ประเภทสินค้า (ที่นอน/โซฟา/ม่าน/พรม)\n• ขนาด (3ฟุต, 6ฟุต, 2ที่นั่ง, ฯลฯ)\n• ประเภทลูกค้า (ลูกค้าใหม่ หรือ สมาชิก)",
		serviceType, itemType, size)
}

// getNCSPricing returns pricing information for NCS cleaning services (Legacy version for backward compatibility)
func getNCSPricing(serviceType, itemType, size, customerType, packageType string, quantity int) string {
	// Use JSON-based pricing if configuration is loaded
	if pricingConfig != nil {
		return getNCSPricingJSON(serviceType, itemType, size, customerType, packageType, quantity)
	}

	// Fallback to hardcoded pricing if JSON config is not available
	log.Printf("Using fallback hardcoded pricing")
	return getNCSPricingHardcoded(serviceType, itemType, size, customerType, packageType, quantity)
}

// getNCSPricingHardcoded returns pricing information for NCS cleaning services (Legacy hardcoded version)
func getNCSPricingHardcoded(serviceType, itemType, size, customerType, packageType string, quantity int) string {
	log.Printf("getNCSPricing called with: serviceType='%s', itemType='%s', size='%s', customerType='%s', packageType='%s', quantity=%d",
		serviceType, itemType, size, customerType, packageType, quantity)

	// Handle customer type variations (including Thai)
	normalizedCustomerType := strings.ToLower(customerType)
	if normalizedCustomerType == "" || normalizedCustomerType == "new" || normalizedCustomerType == "ลูกค้าใหม่" {
		customerType = "new"
	} else if normalizedCustomerType == "member" || normalizedCustomerType == "เมมเบอร์" || normalizedCustomerType == "สมาชิก" || strings.Contains(normalizedCustomerType, "member") {
		customerType = "member"
	}

	// Handle package type variations (including Thai)
	normalizedPackageType := strings.ToLower(packageType)
	if normalizedPackageType == "" || normalizedPackageType == "regular" || normalizedPackageType == "ปกติ" {
		packageType = "regular"
	} else if normalizedPackageType == "coupon" || normalizedPackageType == "คูปอง" {
		packageType = "coupon"
	} else if normalizedPackageType == "contract" || normalizedPackageType == "สัญญา" {
		packageType = "contract"
	}

	log.Printf("Normalized values: customerType='%s', packageType='%s'", customerType, packageType)

	// New Customer Regular Pricing
	if customerType == "new" {
		if serviceType == "disinfection" || serviceType == "กำจัดเชื้อโรค" {
			switch itemType {
			case "mattress", "ที่นอน":
				// Handle case where size is not specified - return both mattress sizes
				if size == "" {
					return "บริการทำความสะอาดที่นอน กำจัดเชื้อโรค-ไรฝุ่น:\n• ที่นอน 3-3.5ฟุต: 1,990 บาท (ลด 35% = 1,290 บาท, ลด 50% = 995 บาท)\n• ที่นอน 5-6ฟุต: 2,390 บาท (ลด 35% = 1,490 บาท, ลด 50% = 1,195 บาท)\n\nกรุณาระบุขนาดที่นอนเพื่อข้อมูลราคาที่แม่นยำ"
				}
				if size == "3-3.5ft" || size == "3ฟุต" || size == "3.5ฟุต" {
					return "ที่นอน 3-3.5ฟุต บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 1,990 บาท, ลด 35% = 1,290 บาท, ลด 50% = 995 บาท"
				} else if size == "5-6ft" || size == "5ฟุต" || size == "6ฟุต" {
					return "ที่นอน 5-6ฟุต บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 2,390 บาท, ลด 35% = 1,490 บาท, ลด 50% = 1,195 บาท"
				}
			case "sofa", "โซฟา":
				// Handle case where size is not specified - return general sofa pricing
				if size == "" {
					return "บริการทำความสะอาดโซฟา กำจัดเชื้อโรค-ไรฝุ่น:\n• เก้าอี้: 450 บาท (ลด 35% = 295 บาท, ลด 50% = 225 บาท)\n• โซฟา 1ที่นั่ง: 990 บาท (ลด 35% = 650 บาท, ลด 50% = 495 บาท)\n• โซฟา 2ที่นั่ง: 1,690 บาท (ลด 35% = 1,100 บาท, ลด 50% = 845 บาท)\n• โซฟา 3ที่นั่ง: 2,390 บาท (ลด 35% = 1,490 บาท, ลด 50% = 1,195 บาท)\n\nกรุณาระบุขนาดโซฟาเพื่อข้อมูลราคาที่แม่นยำ"
				}
				switch size {
				case "chair", "เก้าอี้":
					return "เก้าอี้ บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 450 บาท, ลด 35% = 295 บาท, ลด 50% = 225 บาท"
				case "1seat", "1ที่นั่ง":
					return "โซฟา 1ที่นั่ง บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 990 บาท, ลด 35% = 650 บาท, ลด 50% = 495 บาท"
				case "2seat", "2ที่นั่ง":
					return "โซฟา 2ที่นั่ง บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 1,690 บาท, ลด 35% = 1,100 บาท, ลด 50% = 845 บาท"
				case "3seat", "3ที่นั่ง":
					return "โซฟา 3ที่นั่ง บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 2,390 บาท, ลด 35% = 1,490 บาท, ลด 50% = 1,195 บาท"
				case "4seat", "4ที่นั่ง":
					return "โซฟา 4ที่นั่ง บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 3,090 บาท, ลด 35% = 1,990 บาท, ลด 50% = 1,545 บาท"
				case "5seat", "5ที่นั่ง":
					return "โซฟา 5ที่นั่ง บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 3,790 บาท, ลด 35% = 2,490 บาท, ลด 50% = 1,895 บาท"
				case "6seat", "6ที่นั่ง":
					return "โซฟา 6ที่นั่ง บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 4,490 บาท, ลด 35% = 2,900 บาท, ลด 50% = 2,245 บาท"
				}
			case "curtain", "ม่าน", "carpet", "พรม", "ม่าน/พรม":
				// Default to per square meter pricing if no size specified
				if size == "" || size == "sqm" || size == "ตรม" || size == "ตร.ม." || size == "ตารางเมตร" || size == "ตารางเมตร(ตรม.)" || size == "ต่อ 1 ตรม" || size == "ต่อ1ตรม" || size == "per_sqm" || size == "per_sqm_disinfection" || size == "1sqm" {
					return "ม่าน/พรม ต่อ 1 ตร.ม. บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 150 บาท, ลด 35% = 95 บาท, ลด 50% = 75 บาท"
				}
			}
		} else if serviceType == "washing" || serviceType == "ซักขจัดคราบ" {
			switch itemType {
			case "mattress", "ที่นอน":
				if size == "3-3.5ft" || size == "3ฟุต" || size == "3.5ฟุต" {
					return "ที่นอน 3-3.5ฟุต บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 2,500 บาท, ลด 35% = 1,590 บาท, ลด 50% = 1,250 บาท"
				} else if size == "5-6ft" || size == "5ฟุต" || size == "6ฟุต" {
					return "ที่นอน 5-6ฟุต บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 2,790 บาท, ลด 35% = 1,790 บาท, ลด 50% = 1,395 บาท"
				}
			case "sofa", "โซฟา":
				switch size {
				case "chair", "เก้าอี้":
					return "เก้าอี้ บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 990 บาท, ลด 35% = 650 บาท, ลด 50% = 495 บาท"
				case "1seat", "1ที่นั่ง":
					return "โซฟา 1ที่นั่ง บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 1,690 บาท, ลด 35% = 1,100 บาท, ลด 50% = 845 บาท"
				case "2seat", "2ที่นั่ง":
					return "โซฟา 2ที่นั่ง บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 2,390 บาท, ลด 35% = 1,490 บาท, ลด 50% = 1,195 บาท"
				case "3seat", "3ที่นั่ง":
					return "โซฟา 3ที่นั่ง บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 3,090 บาท, ลด 35% = 1,990 บาท, ลด 50% = 1,545 บาท"
				case "4seat", "4ที่นั่ง":
					return "โซฟา 4ที่นั่ง บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 3,790 บาท, ลด 35% = 2,490 บาท, ลด 50% = 1,895 บาท"
				case "5seat", "5ที่นั่ง":
					return "โซฟา 5ที่นั่ง บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 4,490 บาท, ลด 35% = 2,900 บาท, ลด 50% = 2,245 บาท"
				case "6seat", "6ที่นั่ง":
					return "โซฟา 6ที่นั่ง บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 5,190 บาท, ลด 35% = 3,350 บาท, ลด 50% = 2,595 บาท"
				}
			case "curtain", "ม่าน", "carpet", "พรม", "ม่าน/พรม":
				if size == "sqm" || size == "ตรม" || size == "ตร.ม." || size == "ตารางเมตร" || size == "ตารางเมตร(ตรม.)" || size == "ต่อ 1 ตรม" || size == "ต่อ1ตรม" || size == "per_sqm" || size == "1sqm" {
					return "ม่าน/พรม ต่อ 1 ตร.ม. บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 700 บาท, ลด 35% = 450 บาท, ลด 50% = 350 บาท"
				}
			}
		}
	}

	// Package Pricing - Coupon Packages
	if packageType == "coupon" || packageType == "คูปอง" {
		if serviceType == "disinfection" || serviceType == "กำจัดเชื้อโรค" {
			switch quantity {
			case 5:
				return "แพคเพจคูปอง 5 ใบ บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 12,950 บาท, ส่วนลด 7,460 บาท, ราคาขาย 5,490 บาท (เฉลี่ย 1,098 บาท/ใบ)"
			case 10:
				return "แพคเพจคูปอง 10 ใบ บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 25,900 บาท, ส่วนลด 16,000 บาท, ราคาขาย 9,900 บาท (เฉลี่ย 990 บาท/ใบ)"
			case 20:
				return "แพคเพจคูปอง 20 ใบ บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 51,800 บาท, ส่วนลด 32,800 บาท, ราคาขาย 19,000 บาท (เฉลี่ย 950 บาท/ใบ)"
			}
		} else if serviceType == "washing" || serviceType == "ซักขจัดคราบ" {
			switch quantity {
			case 5:
				return "แพคเพจคูปอง 5 ใบ บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 13,500 บาท, ส่วนลด 6,550 บาท, ราคาขาย 6,950 บาท (เฉลี่ย 1,390 บาท/ใบ)"
			case 10:
				return "แพคเพจคูปอง 10 ใบ บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 27,000 บาท, ส่วนลด 14,100 บาท, ราคาขาย 12,900 บาท (เฉลี่ย 1,290 บาท/ใบ)"
			}
		}
	}

	// Contract/Annual Package Pricing
	if packageType == "contract" || packageType == "สัญญา" {
		if serviceType == "disinfection" || serviceType == "กำจัดเชื้อโรค" {
			switch quantity {
			case 2:
				return "สัญญา 2 ชิ้น บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 4,780 บาท, ส่วนลด 2,090 บาท, ราคาขาย 2,690 บาท (เฉลี่ย 1,345 บาท/ชิ้น) มัดจำขั้นต่ำ 1,000 บาท"
			case 3:
				return "สัญญา 3 ชิ้น บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 7,170 บาท, ส่วนลด 3,520 บาท, ราคาขาย 3,850 บาท (เฉลี่ย 1,283 บาท/ชิ้น) มัดจำขั้นต่ำ 1,000 บาท"
			case 4:
				return "สัญญา 4 ชิ้น บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 9,560 บาท, ส่วนลด 4,870 บาท, ราคาขาย 4,690 บาท (เฉลี่ย 1,173 บาท/ชิ้น) มัดจำขั้นต่ำ 1,000 บาท"
			case 5:
				return "สัญญา 5 ชิ้น บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 11,950 บาท, ส่วนลด 6,860 บาท, ราคาขาย 5,450 บาท (เฉลี่ย 1,090 บาท/ชิ้น) มัดจำขั้นต่ำ 1,000 บาท"
			}
		}
	}

	// Member Pricing
	if customerType == "member" || customerType == "เมมเบอร์" || customerType == "สมาชิก" || strings.Contains(strings.ToLower(customerType), "member") {
		if serviceType == "disinfection" || serviceType == "กำจัดเชื้อโรค" {
			switch itemType {
			case "mattress", "ที่นอน":
				if size == "3-3.5ft" || size == "3ฟุต" || size == "3.5ฟุต" {
					return "ที่นอน 3-3.5ฟุต สำหรับสมาชิก NCS Family Member บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 1,990 บาท, ราคาลด 50% = 995 บาท"
				} else if size == "5-6ft" || size == "5ฟุต" || size == "6ฟุต" {
					return "ที่นอน 5-6ฟุต สำหรับสมาชิก NCS Family Member บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 2,390 บาท, ราคาลด 50% = 1,195 บาท"
				}
			case "sofa", "โซฟา":
				switch size {
				case "chair", "เก้าอี้":
					return "เก้าอี้ สำหรับสมาชิก NCS Family Member บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 450 บาท, ราคาลด 50% = 225 บาท"
				case "1seat", "1ที่นั่ง":
					return "โซฟา 1ที่นั่ง สำหรับสมาชิก NCS Family Member บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 990 บาท, ราคาลด 50% = 495 บาท"
				case "2seat", "2ที่นั่ง":
					return "โซฟา 2ที่นั่ง สำหรับสมาชิก NCS Family Member บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 1,690 บาท, ราคาลด 50% = 845 บาท"
				case "3seat", "3ที่นั่ง":
					return "โซฟา 3ที่นั่ง สำหรับสมาชิก NCS Family Member บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 2,390 บาท, ราคาลด 50% = 1,195 บาท"
				case "4seat", "4ที่นั่ง":
					return "โซฟา 4ที่นั่ง สำหรับสมาชิก NCS Family Member บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 3,090 บาท, ราคาลด 50% = 1,545 บาท"
				case "5seat", "5ที่นั่ง":
					return "โซฟา 5ที่นั่ง สำหรับสมาชิก NCS Family Member บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 3,790 บาท, ราคาลด 50% = 1,895 บาท"
				case "6seat", "6ที่นั่ง":
					return "โซฟา 6ที่นั่ง สำหรับสมาชิก NCS Family Member บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 4,490 บาท, ราคาลด 50% = 2,245 บาท"
				}
			case "curtain", "ม่าน", "carpet", "พรม", "ม่าน/พรม":
				if size == "sqm" || size == "ตรม" || size == "ตร.ม." || size == "ตารางเมตร" || size == "per_sqm" || size == "1sqm" {
					return "ม่าน/พรม ต่อ 1 ตร.ม. สำหรับสมาชิก NCS Family Member บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 150 บาท, ราคาลด 50% = 75 บาท"
				}
			}
		} else if serviceType == "washing" || serviceType == "ซักขจัดคราบ" {
			switch itemType {
			case "mattress", "ที่นอน":
				if size == "3-3.5ft" || size == "3ฟุต" || size == "3.5ฟุต" {
					return "ที่นอน 3-3.5ฟุต สำหรับสมาชิก NCS Family Member บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 2,500 บาท, ราคาลด 50% = 1,250 บาท"
				} else if size == "5-6ft" || size == "5ฟุต" || size == "6ฟุต" {
					return "ที่นอน 5-6ฟุต สำหรับสมาชิก NCS Family Member บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 2,790 บาท, ราคาลด 50% = 1,395 บาท"
				}
			case "sofa", "โซฟา":
				switch size {
				case "chair", "เก้าอี้":
					return "เก้าอี้ สำหรับสมาชิก NCS Family Member บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 990 บาท, ราคาลด 50% = 495 บาท"
				case "1seat", "1ที่นั่ง":
					return "โซฟา 1ที่นั่ง สำหรับสมาชิก NCS Family Member บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 1,690 บาท, ราคาลด 50% = 845 บาท"
				case "2seat", "2ที่นั่ง":
					return "โซฟา 2ที่นั่ง สำหรับสมาชิก NCS Family Member บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 2,390 บาท, ราคาลด 50% = 1,195 บาท"
				case "3seat", "3ที่นั่ง":
					return "โซฟา 3ที่นั่ง สำหรับสมาชิก NCS Family Member บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 3,090 บาท, ราคาลด 50% = 1,545 บาท"
				case "4seat", "4ที่นั่ง":
					return "โซฟา 4ที่นั่ง สำหรับสมาชิก NCS Family Member บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 3,790 บาท, ราคาลด 50% = 1,895 บาท"
				case "5seat", "5ที่นั่ง":
					return "โซฟา 5ที่นั่ง สำหรับสมาชิก NCS Family Member บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 4,490 บาท, ราคาลด 50% = 2,245 บาท"
				case "6seat", "6ที่นั่ง":
					return "โซฟา 6ที่นั่ง สำหรับสมาชิก NCS Family Member บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 5,190 บาท, ราคาลด 50% = 2,595 บาท"
				}
			case "curtain", "ม่าน", "carpet", "พรม", "ม่าน/พรม":
				if size == "sqm" || size == "ตรม" || size == "ตร.ม." || size == "ตารางเมตร" || size == "per_sqm" || size == "1sqm" {
					return "ม่าน/พรม ต่อ 1 ตร.ม. สำหรับสมาชิก NCS Family Member บริการซักขจัดคราบ-กลิ่น: ราคาเต็ม 700 บาท, ราคาลด 50% = 350 บาท"
				}
			}
		}
	}

	return "ขออภัย ไม่พบข้อมูลราคาสำหรับบริการที่ระบุ กรุณาติดต่อเจ้าหน้าที่เพื่อสอบถามราคาเพิ่มเติม หรือระบุรายละเอียดให้ชัดเจนมากขึ้น เช่น ประเภทบริการ (กำจัดเชื้อโรค หรือ ซักขจัดคราบ), ประเภทสินค้า (ที่นอน/โซฟา), ขนาด, และประเภทลูกค้า"
}

func replyToLine(replyToken, message string) {
	if message == "" {
		log.Println("No message to reply.")
		return
	}
	lineReplyURL := "https://api.line.me/v2/bot/message/reply"
	channelToken := os.Getenv("LINE_CHANNEL_ACCESS_TOKEN")
	if channelToken == "" {
		log.Println("LINE channel access token not set.")
		return
	}
	payload := map[string]interface{}{
		"replyToken": replyToken,
		"messages": []map[string]string{{
			"type": "text",
			"text": message,
		}},
	}
	jsonPayload, _ := json.Marshal(payload)
	client := &http.Client{}
	req, _ := http.NewRequest("POST", lineReplyURL, io.NopCloser(bytes.NewReader(jsonPayload)))
	req.Header.Set("Authorization", "Bearer "+channelToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error replying to LINE:", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Println("LINE reply error:", string(body))
	}
}
