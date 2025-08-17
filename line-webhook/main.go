package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

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
	app := fiber.New()

	app.Post("/webhook", func(c *fiber.Ctx) error {
		var event LineEvent
		if err := json.Unmarshal(c.Body(), &event); err != nil {
			return c.SendStatus(fiber.StatusBadRequest)
		}
		for _, e := range event.Events {
			if e.Type == "message" && e.Message.Type == "text" {
				userId := e.Source.UserID
				userThreadLock.Lock()
				userMsgBuffer[userId] = append(userMsgBuffer[userId], e.Message.Text)
				if timer, ok := userMsgTimer[userId]; ok {
					timer.Stop()
				}
				t := time.AfterFunc(10*time.Second, func() {
					userThreadLock.Lock()
					msgs := userMsgBuffer[userId]
					userMsgBuffer[userId] = nil
					userThreadLock.Unlock()
					if len(msgs) == 0 {
						return
					}
					summary := msgs[0]
					if len(msgs) > 1 {
						summary = "สรุปคำถามลูกค้า: " + fmt.Sprintf("%v", msgs)
					}
					responseText := getAssistantResponse(userId, summary)
					replyToLine(e.ReplyToken, responseText)
				})
				userMsgTimer[userId] = t
				userThreadLock.Unlock()
			}
		}
		return c.SendStatus(fiber.StatusOK)
	})

	log.Fatal(app.Listen(":8080"))
}

// getAssistantResponse uses OpenAI Assistants API, mapping userId to threadId in-memory
func getAssistantResponse(userId, message string) string {
	// Check for duplicate question
	userThreadLock.Lock()
	lastQA, hasLast := userLastQAMap[userId]
	userThreadLock.Unlock()
	if hasLast && lastQA.Question == message {
		return "You already asked this question."
	}

	apiKey := os.Getenv("CHATGPT_API_KEY")
	if apiKey == "" {
		return "OpenAI API key not configured."
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
			return "Error creating thread."
		}
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
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

	// Get current time in Asia/Bangkok
	timeResp, err := http.Get("https://timeapi.io/api/Time/current/zone?timeZone=Asia/Bangkok")
	var timeStr string
	if err == nil {
		defer timeResp.Body.Close()
		timeBody, _ := ioutil.ReadAll(timeResp.Body)
		var timeObj struct {
			DateTime string `json:"dateTime"`
		}
		json.Unmarshal(timeBody, &timeObj)
		if timeObj.DateTime != "" {
			timeStr = timeObj.DateTime
		}
	}

	// Add message to thread (with current time for GPT)
	msgReq := map[string]interface{}{
		"role":    "user",
		"content": fmt.Sprintf("ขณะนี้เวลา %s: %s", timeStr, message),
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
	body, _ := ioutil.ReadAll(msgResp.Body)
	var msgRespObj map[string]interface{}
	json.Unmarshal(body, &msgRespObj)

	// Run the assistant
	assistantId := os.Getenv("OPENAI_ASSISTANT_ID")
	if assistantId == "" {
		return "OPENAI_ASSISTANT_ID not set."
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
		return "Error running assistant."
	}
	defer runResp.Body.Close()
	body, _ = ioutil.ReadAll(runResp.Body)
	var runRespObj struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	json.Unmarshal(body, &runRespObj)
	if runRespObj.ID == "" {
		return "Failed to start run."
	}

	// Poll run status and get response
	for i := 0; i < 20; i++ {
		runStatusUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs/" + runRespObj.ID
		runStatusReq, _ := http.NewRequest("GET", runStatusUrl, nil)
		runStatusReq.Header.Set("Authorization", "Bearer "+apiKey)
		runStatusReq.Header.Set("OpenAI-Beta", "assistants=v2")
		runStatusResp, err := client.Do(runStatusReq)
		if err != nil {
			return "Error polling run status."
		}
		defer runStatusResp.Body.Close()
		statusBody, _ := ioutil.ReadAll(runStatusResp.Body)
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
		fmt.Println("Run status:", statusObj.Status)
		// --- เช็ค required_action.submit_tool_outputs.tool_calls ใน /runs ---
		if statusObj.RequiredAction.Type == "submit_tool_outputs" && len(statusObj.RequiredAction.SubmitToolOutputs.ToolCalls) > 0 {
			for _, call := range statusObj.RequiredAction.SubmitToolOutputs.ToolCalls {
				if call.Function.Name == "get_available_slots_with_months" {
					var argStr string
					json.Unmarshal(call.Function.Arguments, &argStr)
					var month string
					json.Unmarshal([]byte(argStr), &struct {
						ThaiMonthYear *string `json:"thai_month_year"`
					}{&month})
					if month != "" {
						url := "https://script.google.com/macros/s/AKfycbwfSkwsgO56UdPHqa-KCxO7N-UDzkiMIBVjBTd0k8sowLtm7wORC-lN32IjAwtOVqMxQw/exec?sheet=" + url.QueryEscape(month)
						resp, err := http.Get(url)
						if err != nil {
							return "Error calling Google Apps Script."
						}
						defer resp.Body.Close()
						gsBody, _ := ioutil.ReadAll(resp.Body)
						result := string(gsBody)
						toolOutputs := []map[string]interface{}{{"tool_call_id": call.ID, "output": result}}
						toolOutputsJson, _ := json.Marshal(map[string]interface{}{"tool_outputs": toolOutputs})
						submitUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs/" + runRespObj.ID + "/submit_tool_outputs"
						submitReq, _ := http.NewRequest("POST", submitUrl, bytes.NewReader(toolOutputsJson))
						submitReq.Header.Set("Authorization", "Bearer "+apiKey)
						submitReq.Header.Set("Content-Type", "application/json")
						submitReq.Header.Set("OpenAI-Beta", "assistants=v2")
						client.Do(submitReq)
					}
				} else if call.Function.Name == "get_ncs_pricing" {
					var argStr string
					json.Unmarshal(call.Function.Arguments, &argStr)
					var args struct {
						ServiceType  string `json:"service_type"`
						ItemType     string `json:"item_type"`
						Size         string `json:"size"`
						CustomerType string `json:"customer_type"`
						PackageType  string `json:"package_type"`
						Quantity     int    `json:"quantity"`
					}
					json.Unmarshal([]byte(argStr), &args)

					result := getNCSPricing(args.ServiceType, args.ItemType, args.Size, args.CustomerType, args.PackageType, args.Quantity)
					toolOutputs := []map[string]interface{}{{"tool_call_id": call.ID, "output": result}}
					toolOutputsJson, _ := json.Marshal(map[string]interface{}{"tool_outputs": toolOutputs})
					submitUrl := "https://api.openai.com/v1/threads/" + threadId + "/runs/" + runRespObj.ID + "/submit_tool_outputs"
					submitReq, _ := http.NewRequest("POST", submitUrl, bytes.NewReader(toolOutputsJson))
					submitReq.Header.Set("Authorization", "Bearer "+apiKey)
					submitReq.Header.Set("Content-Type", "application/json")
					submitReq.Header.Set("OpenAI-Beta", "assistants=v2")
					client.Do(submitReq)
				}
			}
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
	body, _ = ioutil.ReadAll(getMsgResp.Body)
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
				if reply != "" {
					// Store last Q&A
					userThreadLock.Lock()
					userLastQAMap[userId] = struct {
						Question string
						Answer   string
					}{Question: message, Answer: reply}
					userThreadLock.Unlock()
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
						if args.ThaiMonthYear != "" {
							month := args.ThaiMonthYear
							// Call Google Apps Script
							url := "https://script.google.com/macros/s/AKfycbwfSkwsgO56UdPHqa-KCxO7N-UDzkiMIBVjBTd0k8sowLtm7wORC-lN32IjAwtOVqMxQw/exec?sheet=" + month
							resp, err := http.Get(url)
							if err != nil {
								return "Error calling Google Apps Script."
							}
							defer resp.Body.Close()
							gsBody, _ := ioutil.ReadAll(resp.Body)
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
							_, _ = ioutil.ReadAll(msgResp.Body)

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
							_, _ = ioutil.ReadAll(runResp.Body)

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
								statusBody, _ := ioutil.ReadAll(runStatusResp.Body)
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
							body, _ := ioutil.ReadAll(getMsgResp.Body)
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

// getNCSPricing returns pricing information for NCS cleaning services
func getNCSPricing(serviceType, itemType, size, customerType, packageType string, quantity int) string {
	// New Customer Regular Pricing
	if customerType == "new" || customerType == "" {
		if serviceType == "disinfection" || serviceType == "กำจัดเชื้อโรค" {
			switch itemType {
			case "mattress", "ที่นอน":
				if size == "3-3.5ft" || size == "3ฟุต" || size == "3.5ฟุต" {
					return "ที่นอน 3-3.5ฟุต บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 1,990 บาท, ลด 35% = 1,290 บาท, ลด 50% = 995 บาท"
				} else if size == "5-6ft" || size == "5ฟุต" || size == "6ฟุต" {
					return "ที่นอน 5-6ฟุต บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 2,390 บาท, ลด 35% = 1,490 บาท, ลด 50% = 1,195 บาท"
				}
			case "sofa", "โซฟา":
				switch size {
				case "chair", "เก้าอี้":
					return "เก้าอี้ บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 450 บาท, ลด 35% = 295 บาท, ลด 50% = 225 บาท"
				case "1seat", "1ที่นั่ง":
					return "โซฟา 1ที่นั่ง บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 990 บาท, ลด 35% = 650 บาท, ลด 50% = 495 บาท"
				case "2seat", "2ที่นั่ง":
					return "โซฟา 2ที่นั่ง บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 1,690 บาท, ลด 35% = 1,100 บาท, ลด 50% = 845 บาท"
				case "3seat", "3ที่นั่ง":
					return "โซฟา 3ที่นั่ง บริการกำจัดเชื้อโรค-ไรฝุ่น: ราคาเต็ม 2,390 บาท, ลด 35% = 1,490 บาท, ลด 50% = 1,195 บาท"
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
	if customerType == "member" || customerType == "เมมเบอร์" {
		if serviceType == "disinfection" || serviceType == "กำจัดเชื้อโรค" {
			switch itemType {
			case "mattress", "ที่นอน":
				if size == "3-3.5ft" || size == "3ฟุต" || size == "3.5ฟุต" {
					return "ที่นอน 3-3.5ฟุต สำหรับสมาชิก NCS Family Member: ราคาเต็ม 1,990 บาท, ราคาลด 50% = 995 บาท"
				} else if size == "5-6ft" || size == "5ฟุต" || size == "6ฟุต" {
					return "ที่นอน 5-6ฟุต สำหรับสมาชิก NCS Family Member: ราคาเต็ม 2,390 บาท, ราคาลด 50% = 1,195 บาท"
				}
			}
		} else if serviceType == "washing" || serviceType == "ซักขจัดคราบ" {
			switch itemType {
			case "mattress", "ที่นอน":
				if size == "3-3.5ft" || size == "3ฟุต" || size == "3.5ฟุต" {
					return "ที่นอน 3-3.5ฟุต สำหรับสมาชิก NCS Family Member: ราคาเต็ม 2,500 บาท, ราคาลด 50% = 1,250 บาท"
				} else if size == "5-6ft" || size == "5ฟุต" || size == "6ฟุต" {
					return "ที่นอน 5-6ฟุต สำหรับสมาชิก NCS Family Member: ราคาเต็ม 2,790 บาท, ราคาลด 50% = 1,395 บาท"
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
	req, _ := http.NewRequest("POST", lineReplyURL, ioutil.NopCloser(bytes.NewReader(jsonPayload)))
	req.Header.Set("Authorization", "Bearer "+channelToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error replying to LINE:", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Println("LINE reply error:", string(body))
	}
}
