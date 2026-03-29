package main

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

//go:embed admin-ui
var adminUI embed.FS

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

// ConversationMessage stores a single message in a conversation history
type ConversationMessage struct {
	Role      string `json:"role"` // "customer", "ai", "admin"
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"` // Bangkok time
}

// UserConversation tracks the full state for a LINE user conversation
type UserConversation struct {
	UserID     string                `json:"user_id"`
	Messages   []ConversationMessage `json:"messages"`
	Takeover   bool                  `json:"takeover"`    // human agent took over
	WantsHuman bool                  `json:"wants_human"` // customer requested a human
	LastSeen   string                `json:"last_seen"`
}

func (c *UserConversation) appendMessage(role, text string) {
	c.Messages = append(c.Messages, ConversationMessage{
		Role:      role,
		Text:      text,
		Timestamp: getBangkokTime(),
	})
	const maxConvMessages = 200
	if len(c.Messages) > maxConvMessages {
		c.Messages = c.Messages[len(c.Messages)-maxConvMessages:]
	}
}

const pricingConfigFile = "pricing_config.json"

var pricingConfig *PricingConfig

type UpdatePriceRequest struct {
	ServiceKey  string      `json:"service_key"`
	ItemKey     string      `json:"item_key"`
	SizeKey     string      `json:"size_key"`
	CustomerKey string      `json:"customer_key"`
	PackageKey  string      `json:"package_key"`
	Price       PriceConfig `json:"price"`
}

type UpdatePromotionRequest struct {
	PackageKey string       `json:"package_key"`
	ServiceKey string       `json:"service_key"`
	Quantity   int          `json:"quantity"`
	Price      PackagePrice `json:"price"`
}

func (r *UpdatePriceRequest) normalize() {
	r.ServiceKey = strings.TrimSpace(r.ServiceKey)
	r.ItemKey = strings.TrimSpace(r.ItemKey)
	r.SizeKey = strings.TrimSpace(r.SizeKey)
	r.CustomerKey = strings.TrimSpace(r.CustomerKey)
	r.PackageKey = strings.TrimSpace(r.PackageKey)
	if r.PackageKey == "" {
		r.PackageKey = "regular"
	}
}

func (r UpdatePriceRequest) validate() error {
	if r.ServiceKey == "" {
		return errors.New("service_key is required")
	}
	if r.ItemKey == "" {
		return errors.New("item_key is required")
	}
	if r.SizeKey == "" {
		return errors.New("size_key is required")
	}
	if r.CustomerKey == "" {
		return errors.New("customer_key is required")
	}
	if !priceHasValue(r.Price) {
		return errors.New("price must include at least one value")
	}
	return nil
}

func (r *UpdatePromotionRequest) normalize() {
	r.PackageKey = strings.TrimSpace(r.PackageKey)
	r.ServiceKey = strings.TrimSpace(r.ServiceKey)
}

func (r UpdatePromotionRequest) validate() error {
	if r.PackageKey == "" {
		return errors.New("package_key is required")
	}
	if r.ServiceKey == "" {
		return errors.New("service_key is required")
	}
	if r.Quantity <= 0 {
		return errors.New("quantity must be greater than zero")
	}
	if !packagePriceHasValue(r.Price) {
		return errors.New("price must include at least one field")
	}
	return nil
}

func priceHasValue(p PriceConfig) bool {
	return p.FullPrice > 0 || p.Discount35 > 0 || p.Discount50 > 0
}

func packagePriceHasValue(p PackagePrice) bool {
	return p.FullPrice > 0 || p.Discount > 0 || p.SalePrice > 0 || p.PerItem > 0
}

// loadPricingConfig loads pricing configuration from JSON file
func loadPricingConfig() error {
	data, err := os.ReadFile(pricingConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read pricing config: %v", err)
	}

	pricingConfig = &PricingConfig{}
	if err := json.Unmarshal(data, pricingConfig); err != nil {
		return fmt.Errorf("failed to parse pricing config: %v", err)
	}
	sanitizePricingConfig(pricingConfig)

	log.Println("Pricing configuration loaded successfully")
	return nil
}

func sanitizePricingConfig(cfg *PricingConfig) {
	if cfg == nil {
		return
	}
	if cfg.Services == nil {
		cfg.Services = make(map[string]ServiceConfig)
	}
	if cfg.Items == nil {
		cfg.Items = make(map[string]ItemConfig)
	}
	if cfg.Packages == nil {
		cfg.Packages = make(map[string]PackageConfig)
	}
	if cfg.CustomerTypes == nil {
		cfg.CustomerTypes = make(map[string]CustomerTypeConfig)
	}
	for itemKey, item := range cfg.Items {
		if item.Sizes == nil {
			item.Sizes = make(map[string]SizeConfig)
		}
		for sizeKey, sizeCfg := range item.Sizes {
			if sizeCfg.Pricing == nil {
				sizeCfg.Pricing = make(map[string]map[string]map[string]PriceConfig)
			}
			for serviceKey, customerMap := range sizeCfg.Pricing {
				if customerMap == nil {
					sizeCfg.Pricing[serviceKey] = make(map[string]map[string]PriceConfig)
					continue
				}
				for customerKey, packageMap := range customerMap {
					if packageMap == nil {
						customerMap[customerKey] = make(map[string]PriceConfig)
					}
				}
			}
			item.Sizes[sizeKey] = sizeCfg
		}
		cfg.Items[itemKey] = item
	}
	for pkgKey, pkg := range cfg.Packages {
		if pkg.Disinfection == nil {
			pkg.Disinfection = make(map[string]PackagePrice)
		}
		if pkg.Washing == nil {
			pkg.Washing = make(map[string]PackagePrice)
		}
		cfg.Packages[pkgKey] = pkg
	}
}

func savePricingConfigToFile(cfg *PricingConfig) error {
	if cfg == nil {
		return errors.New("pricing config is nil")
	}
	sanitizePricingConfig(cfg)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal pricing config: %w", err)
	}
	tmpPath := pricingConfigFile + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp config: %w", err)
	}
	if err := os.Rename(tmpPath, pricingConfigFile); err != nil {
		return fmt.Errorf("failed to replace pricing config: %w", err)
	}
	return nil
}

func clonePricingConfig(cfg *PricingConfig) (*PricingConfig, error) {
	if cfg == nil {
		return nil, errors.New("pricing config is nil")
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to copy pricing config: %w", err)
	}
	clone := &PricingConfig{}
	if err := json.Unmarshal(data, clone); err != nil {
		return nil, fmt.Errorf("failed to rebuild clone: %w", err)
	}
	sanitizePricingConfig(clone)
	return clone, nil
}

func respondError(c *fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(fiber.Map{"error": message})
}

func adminAuthMiddleware(c *fiber.Ctx) error {
	adminToken := os.Getenv("ADMIN_API_TOKEN")
	if adminToken == "" {
		log.Printf("ADMIN_API_TOKEN is not configured; rejecting admin request from %s", c.IP())
		return respondError(c, fiber.StatusForbidden, "admin API is disabled")
	}
	provided := c.Get("X-Admin-Token")
	if provided == "" || provided != adminToken {
		return respondError(c, fiber.StatusUnauthorized, "invalid admin token")
	}
	return c.Next()
}

func handleGetPricingConfig(c *fiber.Ctx) error {
	if pricingConfig == nil {
		return respondError(c, fiber.StatusServiceUnavailable, "pricing config not loaded")
	}
	return c.JSON(pricingConfig)
}

func handleReplacePricingConfig(c *fiber.Ctx) error {
	var incoming PricingConfig
	if err := c.BodyParser(&incoming); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid JSON payload")
	}
	sanitizePricingConfig(&incoming)
	if err := savePricingConfigToFile(&incoming); err != nil {
		log.Printf("Failed to persist pricing config: %v", err)
		return respondError(c, fiber.StatusInternalServerError, "unable to save pricing config")
	}
	pricingConfig = &incoming
	return c.JSON(fiber.Map{
		"status": "ok",
		"config": pricingConfig,
	})
}

func handleUpdatePriceEntry(c *fiber.Ctx) error {
	if pricingConfig == nil {
		return respondError(c, fiber.StatusServiceUnavailable, "pricing config not loaded")
	}
	var req UpdatePriceRequest
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid JSON payload")
	}
	req.normalize()
	if err := req.validate(); err != nil {
		return respondError(c, fiber.StatusBadRequest, err.Error())
	}
	workingCopy, err := clonePricingConfig(pricingConfig)
	if err != nil {
		log.Printf("Failed to clone pricing config: %v", err)
		return respondError(c, fiber.StatusInternalServerError, "unable to prepare pricing config")
	}
	if err := applyPriceUpdate(workingCopy, req); err != nil {
		return respondError(c, fiber.StatusBadRequest, err.Error())
	}
	if err := savePricingConfigToFile(workingCopy); err != nil {
		log.Printf("Failed to save pricing config: %v", err)
		return respondError(c, fiber.StatusInternalServerError, "unable to persist pricing config")
	}
	pricingConfig = workingCopy
	return c.JSON(fiber.Map{
		"status": "ok",
		"price":  req.Price,
	})
}

func handleUpdatePromotionEntry(c *fiber.Ctx) error {
	if pricingConfig == nil {
		return respondError(c, fiber.StatusServiceUnavailable, "pricing config not loaded")
	}
	var req UpdatePromotionRequest
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid JSON payload")
	}
	req.normalize()
	if err := req.validate(); err != nil {
		return respondError(c, fiber.StatusBadRequest, err.Error())
	}
	workingCopy, err := clonePricingConfig(pricingConfig)
	if err != nil {
		log.Printf("Failed to clone pricing config: %v", err)
		return respondError(c, fiber.StatusInternalServerError, "unable to prepare pricing config")
	}
	if err := applyPromotionUpdate(workingCopy, req); err != nil {
		return respondError(c, fiber.StatusBadRequest, err.Error())
	}
	if err := savePricingConfigToFile(workingCopy); err != nil {
		log.Printf("Failed to save pricing config: %v", err)
		return respondError(c, fiber.StatusInternalServerError, "unable to persist pricing config")
	}
	pricingConfig = workingCopy
	return c.JSON(fiber.Map{
		"status":    "ok",
		"promotion": req.Price,
	})
}

func applyPriceUpdate(cfg *PricingConfig, req UpdatePriceRequest) error {
	service, ok := cfg.Services[req.ServiceKey]
	if !ok {
		return fmt.Errorf("unknown service_key '%s'", req.ServiceKey)
	}
	_ = service // service variable kept for future logging/validation
	item, ok := cfg.Items[req.ItemKey]
	if !ok {
		return fmt.Errorf("unknown item_key '%s'", req.ItemKey)
	}
	sizeCfg, ok := item.Sizes[req.SizeKey]
	if !ok {
		return fmt.Errorf("unknown size_key '%s'", req.SizeKey)
	}
	if sizeCfg.Pricing == nil {
		sizeCfg.Pricing = make(map[string]map[string]map[string]PriceConfig)
	}
	if _, ok := sizeCfg.Pricing[req.ServiceKey]; !ok {
		sizeCfg.Pricing[req.ServiceKey] = make(map[string]map[string]PriceConfig)
	}
	customerPricing := sizeCfg.Pricing[req.ServiceKey]
	if _, ok := customerPricing[req.CustomerKey]; !ok {
		customerPricing[req.CustomerKey] = make(map[string]PriceConfig)
	}
	customerPricing[req.CustomerKey][req.PackageKey] = req.Price
	sizeCfg.Pricing[req.ServiceKey] = customerPricing
	item.Sizes[req.SizeKey] = sizeCfg
	cfg.Items[req.ItemKey] = item
	return nil
}

func applyPromotionUpdate(cfg *PricingConfig, req UpdatePromotionRequest) error {
	pkg, ok := cfg.Packages[req.PackageKey]
	if !ok {
		return fmt.Errorf("unknown package_key '%s'", req.PackageKey)
	}
	quantityKey := strconv.Itoa(req.Quantity)
	switch req.ServiceKey {
	case "disinfection":
		if pkg.Disinfection == nil {
			pkg.Disinfection = make(map[string]PackagePrice)
		}
		pkg.Disinfection[quantityKey] = req.Price
	case "washing":
		if pkg.Washing == nil {
			pkg.Washing = make(map[string]PackagePrice)
		}
		pkg.Washing[quantityKey] = req.Price
	default:
		return fmt.Errorf("service_key '%s' is not supported for promotions", req.ServiceKey)
	}
	cfg.Packages[req.PackageKey] = pkg
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

	userConversations = make(map[string]*UserConversation) // conversation history per user
)

func main() {
	// Load pricing configuration
	if err := loadPricingConfig(); err != nil {
		log.Fatal("Failed to load pricing configuration:", err)
	}

	app := fiber.New()

	// Serve embedded admin UI files
	app.Get("/admin-ui/", func(c *fiber.Ctx) error {
		data, err := adminUI.ReadFile("admin-ui/index.html")
		if err != nil {
			return c.Status(404).SendString("Not Found")
		}
		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.Send(data)
	})

	app.Get("/admin-ui/app.js", func(c *fiber.Ctx) error {
		data, err := adminUI.ReadFile("admin-ui/app.js")
		if err != nil {
			return c.Status(404).SendString("Not Found")
		}
		c.Set("Content-Type", "application/javascript; charset=utf-8")
		return c.Send(data)
	})

	app.Get("/admin-ui/styles.css", func(c *fiber.Ctx) error {
		data, err := adminUI.ReadFile("admin-ui/styles.css")
		if err != nil {
			return c.Status(404).SendString("Not Found")
		}
		c.Set("Content-Type", "text/css; charset=utf-8")
		return c.Send(data)
	})

	app.Get("/admin-ui", func(c *fiber.Ctx) error {
		return c.Redirect("/admin-ui/")
	})

	adminGroup := app.Group("/admin", adminAuthMiddleware)
	adminGroup.Get("/config/pricing", handleGetPricingConfig)
	adminGroup.Put("/config/pricing", handleReplacePricingConfig)
	adminGroup.Post("/config/pricing/price", handleUpdatePriceEntry)
	adminGroup.Post("/config/pricing/promotion", handleUpdatePromotionEntry)

	adminGroup.Get("/conversations", handleGetConversations)
	adminGroup.Get("/conversations/:userId", handleGetConversationMessages)
	adminGroup.Post("/conversations/:userId/takeover", handleTakeoverConversation)
	adminGroup.Post("/conversations/:userId/release", handleReleaseConversation)
	adminGroup.Post("/conversations/:userId/reply", handleAdminReply)

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
					log.Printf("Processing image message with ID: %s", e.Message.ID)
					imageURL, err := getLineImageURL(e.Message.ID)
					if err != nil {
						log.Printf("Error getting image URL for message ID %s: %v", e.Message.ID, err)
						messageContent = "ได้รับรูปภาพจากลูกค้า (ไม่สามารถแสดงได้)"
					} else {
						log.Printf("Successfully converted image to data URL. Length: %d", len(imageURL))
						messageContent = "ลูกค้าส่งรูปภาพ: " + imageURL
						log.Printf("Image message content prepared: ลูกค้าส่งรูปภาพ: [DATA_URL]")
					}
				} else {
					// Skip other message types
					continue
				}

				userThreadLock.Lock()
				userMsgBuffer[userId] = append(userMsgBuffer[userId], messageContent)

				// Record customer message in conversation history
				if _, ok := userConversations[userId]; !ok {
					userConversations[userId] = &UserConversation{UserID: userId}
				}
				{
					conv := userConversations[userId]
					conv.LastSeen = getBangkokTime()
					if detectHumanRequest(messageContent) {
						conv.WantsHuman = true
					}
					displayMsg := messageContent
					if strings.Contains(messageContent, "data:image") {
						displayMsg = "[รูปภาพ]"
					}
					conv.appendMessage("customer", displayMsg)
				}

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

					// Check if human takeover is active - skip AI if so
					userThreadLock.Lock()
					takeoverActive := userConversations[userId] != nil && userConversations[userId].Takeover
					userThreadLock.Unlock()
					if takeoverActive {
						log.Printf("Human takeover active for user %s, skipping AI response", userId)
						return
					}

					responseText := getAssistantResponse(userId, summary)
					replyToLine(replyToken, responseText)

					// Record AI response in conversation history
					if responseText != "" {
						userThreadLock.Lock()
						if conv, ok := userConversations[userId]; ok {
							conv.appendMessage("ai", responseText)
						}
						userThreadLock.Unlock()
					}
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
	log.Printf("Starting image download for message ID: %s", messageID)

	channelToken := os.Getenv("LINE_CHANNEL_ACCESS_TOKEN")
	if channelToken == "" {
		log.Printf("ERROR: LINE_CHANNEL_ACCESS_TOKEN not set")
		return "", fmt.Errorf("LINE channel access token not set")
	}
	log.Printf("LINE_CHANNEL_ACCESS_TOKEN found: %s...", channelToken[:10])

	// Get image content from LINE
	imageURL := "https://api-data.line.me/v2/bot/message/" + messageID + "/content"
	log.Printf("Requesting image from: %s", imageURL)

	req, err := http.NewRequest("GET", imageURL, nil)
	if err != nil {
		log.Printf("ERROR: Failed to create request: %v", err)
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+channelToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("ERROR: Failed to download image: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	log.Printf("Image download response status: %d %s", resp.StatusCode, resp.Status)
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("ERROR: Image download failed. Response body: %s", string(body))
		return "", fmt.Errorf("failed to get image: %s", resp.Status)
	}

	// Read image data
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("ERROR: Failed to read image data: %v", err)
		return "", fmt.Errorf("error reading image data: %v", err)
	}
	log.Printf("Image data size: %d bytes", len(imageData))

	// Check if image is too large for OpenAI API (limit ~20MB for data URLs)
	const maxImageSize = 20 * 1024 * 1024 // 20MB
	if len(imageData) > maxImageSize {
		log.Printf("⚠️ Image too large (%d bytes > %d bytes). Attempting to resize...", len(imageData), maxImageSize)

		// Try to compress/resize the image (basic approach)
		// For production, you might want to use a proper image processing library
		// For now, we'll truncate or reject very large images
		return "", fmt.Errorf("รูปภาพมีขนาดใหญ่เกินไป กรุณาลดขนาดรูปภาพแล้วลองใหม่อีกครั้ง")
	}

	// Get content type or default to image/jpeg
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	log.Printf("Image content type: %s", contentType)

	// Convert to base64 data URL for GPT-4 Vision
	base64Data := base64.StdEncoding.EncodeToString(imageData)
	dataURL := fmt.Sprintf("data:%s;base64,%s", contentType, base64Data)

	// Check final data URL length (OpenAI has limits on data URL size)
	const maxDataURLLength = 1000000 // ~1MB base64 encoded
	if len(dataURL) > maxDataURLLength {
		log.Printf("⚠️ Data URL too long (%d chars > %d chars)", len(dataURL), maxDataURLLength)
		return "", fmt.Errorf("รูปภาพมีขนาดใหญ่เกินไป กรุณาลดขนาดรูปภาพแล้วลองใหม่อีกครั้ง")
	}

	log.Printf("✅ Successfully created data URL. Length: %d characters", len(dataURL))

	return dataURL, nil
}

// parseDataURL decodes a data URL (e.g., data:image/jpeg;base64,...) into content type and raw bytes
func parseDataURL(dataURL string) (string, []byte, error) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", nil, fmt.Errorf("not a data URL")
	}
	// Split metadata and payload
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid data URL format")
	}
	meta := parts[0] // e.g., data:image/jpeg;base64
	payload := parts[1]
	if !strings.Contains(meta, ";base64") {
		return "", nil, fmt.Errorf("only base64 data URLs are supported")
	}
	// Extract content type
	meta = strings.TrimPrefix(meta, "data:")
	ct := meta
	if semi := strings.Index(ct, ";"); semi != -1 {
		ct = ct[:semi]
	}
	// Sanitize payload: keep only valid base64 chars (A-Za-z0-9+/=)
	if idx := strings.IndexByte(payload, ' '); idx != -1 {
		payload = payload[:idx]
	}
	b64re := regexp.MustCompile(`[A-Za-z0-9+/=]+`)
	match := b64re.FindString(payload)
	if match == "" {
		return "", nil, fmt.Errorf("no valid base64 payload found")
	}
	// Decode
	decoded, err := base64.StdEncoding.DecodeString(match)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode base64: %w", err)
	}
	return ct, decoded, nil
}

// extractFirstDataURL finds the first valid image data URL in a string and returns it exactly
func extractFirstDataURL(s string) (string, error) {
	// Match data:image/<type>;base64,<payload>
	// base64 payload restricted to valid chars only to avoid trailing list/bracket artifacts
	re := regexp.MustCompile(`data:image/[a-zA-Z0-9.+-]+;base64,[A-Za-z0-9+/=]+`)
	loc := re.FindStringIndex(s)
	if loc == nil {
		return "", fmt.Errorf("no image data URL found")
	}
	return s[loc[0]:loc[1]], nil
}

// uploadImageFileToOpenAI uploads image bytes to OpenAI Files API and returns file_id for Assistants
func uploadImageFileToOpenAI(client *http.Client, apiKey string, filename string, data []byte) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// purpose must be "assistants" for Assistants API
	if err := writer.WriteField("purpose", "assistants"); err != nil {
		return "", fmt.Errorf("failed to write purpose: %w", err)
	}
	fw, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return "", fmt.Errorf("failed to write file data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/files", &buf)
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		log.Printf("❌ File upload failed. Status=%d Body=%s", resp.StatusCode, string(body))
		return "", fmt.Errorf("file upload failed: %s", resp.Status)
	}
	var obj struct {
		ID       string `json:"id"`
		Object   string `json:"object"`
		Filename string `json:"filename"`
	}
	_ = json.Unmarshal(body, &obj)
	if obj.ID == "" {
		return "", fmt.Errorf("file_id missing in upload response")
	}
	log.Printf("✅ Uploaded image to OpenAI. file_id=%s filename=%s", obj.ID, obj.Filename)
	return obj.ID, nil
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

	// Check if message contains image URL - more robust detection
	if strings.Contains(message, "ลูกค้าส่งรูปภาพ:") && strings.Contains(message, "data:image") {
		// Handle image message with vision
		log.Printf("🖼️ DETECTED IMAGE MESSAGE: preparing vision request")

		// Extract the exact data URL safely (avoid trailing characters)
		imageURL, err := extractFirstDataURL(message)
		if err != nil {
			log.Printf("❌ ERROR: %v", err)
			return "ขออภัย ไม่สามารถประมวลผลรูปภาพได้ กรุณาลองใหม่อีกครั้ง"
		}
		log.Printf("🔍 Image URL extracted - Length: %d characters", len(imageURL))

		// Check if image data URL is too large for OpenAI API
		const maxDataURLLength = 1000000 // ~1MB base64 encoded
		if len(imageURL) > maxDataURLLength {
			log.Printf("⚠️ Data URL too long (%d chars > %d chars) - rejecting", len(imageURL), maxDataURLLength)
			return "ขออภัย รูปภาพมีขนาดใหญ่เกินไป กรุณาลดขนาดรูปภาพหรือถ่ายรูปใหม่ให้เล็กกว่านี้แล้วลองใหม่อีกครั้งค่ะ 📸"
		}

		// Validate data URL format
		if !strings.HasPrefix(imageURL, "data:image/") {
			log.Printf("❌ ERROR: Invalid data URL format: %s", imageURL[:50])
			return "ขออภัย รูปแบบรูปภาพไม่ถูกต้อง กรุณาลองใหม่อีกครั้ง"
		} // Show preview of image URL (first 100 chars or less)
		previewLen := 100
		if len(imageURL) < previewLen {
			previewLen = len(imageURL)
		}
		log.Printf("🎯 Image URL preview: %s...", imageURL[:previewLen])

		// Convert data URL to bytes and upload via Files API
		ct, imgBytes, err := parseDataURL(imageURL)
		if err != nil {
			log.Printf("❌ Failed to parse data URL: %v", err)
			return "ขออภัย ไม่สามารถอ่านรูปภาพได้ กรุณาลองใหม่อีกครั้ง"
		}
		// Derive filename extension from content type
		filename := "customer-image"
		if strings.Contains(ct, "jpeg") || strings.Contains(ct, "jpg") {
			filename += ".jpg"
		}
		if strings.Contains(ct, "png") {
			filename += ".png"
		}
		if strings.Contains(ct, "webp") {
			filename += ".webp"
		}

		fileID, err := uploadImageFileToOpenAI(client, apiKey, filename, imgBytes)
		if err != nil {
			log.Printf("❌ Failed to upload image to OpenAI: %v", err)
			return "ขออภัย ระบบอัปโหลดรูปภาพมีปัญหา กรุณาลองใหม่อีกครั้งค่ะ"
		}

		msgReq = map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("ขณะนี้เวลา %s: ลูกค้าส่งรูปภาพมา กรุณาวิเคราะห์รูปภาพและให้คำแนะนำเกี่ยวกับบริการทำความสะอาดที่เหมาะสม", timeStr),
				},
				{
					"type": "image_file",
					"image_file": map[string]string{
						"file_id": fileID,
					},
				},
			},
		}
		log.Printf("✅ VISION REQUEST PREPARED (image_file). file_id=%s", fileID)
	} else {
		// Handle text message
		msgReq = map[string]interface{}{
			"role":    "user",
			"content": fmt.Sprintf("ขณะนี้เวลา %s: %s", timeStr, message),
		}
	}

	msgPayload, _ := json.Marshal(msgReq)
	log.Printf("📤 Sending message to OpenAI thread. Payload size: %d bytes", len(msgPayload))
	if strings.Contains(string(msgPayload), "image_url") {
		log.Printf("🖼️ CONFIRMED: Message contains image_url for vision processing")
	}

	msgUrl := "https://api.openai.com/v1/threads/" + threadId + "/messages"
	msgReqHttp, _ := http.NewRequest("POST", msgUrl, bytes.NewReader(msgPayload))
	msgReqHttp.Header.Set("Authorization", "Bearer "+apiKey)
	msgReqHttp.Header.Set("Content-Type", "application/json")
	msgReqHttp.Header.Set("OpenAI-Beta", "assistants=v2")
	msgResp, err := client.Do(msgReqHttp)
	if err != nil {
		log.Printf("❌ ERROR sending message to thread: %v", err)
		return "Error sending message to thread."
	}
	defer msgResp.Body.Close()
	body, _ := io.ReadAll(msgResp.Body)
	log.Printf("📬 Message sent to thread. Response status: %d", msgResp.StatusCode)
	if msgResp.StatusCode != 200 {
		log.Printf("⚠️ Non-200 response body: %s", string(body))
	}
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
				} else if call.Function.Name == "get_action_step_summary" {
					log.Printf("get_action_step_summary called with arguments: %s", string(call.Function.Arguments))
					var args struct {
						AnalysisType       string `json:"analysis_type"`
						ItemIdentified     string `json:"item_identified"`
						ConditionAssessed  string `json:"condition_assessed,omitempty"`
						RecommendedService string `json:"recommended_service,omitempty"`
					}

					if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
						var argStr string
						if err2 := json.Unmarshal(call.Function.Arguments, &argStr); err2 == nil {
							if err3 := json.Unmarshal([]byte(argStr), &args); err3 != nil {
								log.Printf("Failed to parse get_action_step_summary arguments after double unmarshal: %v", err3)
								aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error parsing step summary arguments: " + err3.Error()})
								continue
							}
						} else {
							log.Printf("Failed to parse get_action_step_summary arguments: %v", err)
							aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error parsing step summary arguments: " + err.Error()})
							continue
						}
					}

					log.Printf("Parsed step summary arguments: AnalysisType='%s', ItemIdentified='%s', ConditionAssessed='%s', RecommendedService='%s'",
						args.AnalysisType, args.ItemIdentified, args.ConditionAssessed, args.RecommendedService)

					result := getActionStepSummary(args.AnalysisType, args.ItemIdentified, args.ConditionAssessed, args.RecommendedService)
					log.Printf("Step summary result: %s", result)
					aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": result})
				} else if call.Function.Name == "get_image_analysis_guidance" {
					log.Printf("get_image_analysis_guidance called with arguments: %s", string(call.Function.Arguments))
					var args struct {
						ImageType       string `json:"image_type,omitempty"`
						AnalysisRequest string `json:"analysis_request,omitempty"`
					}

					if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
						var argStr string
						if err2 := json.Unmarshal(call.Function.Arguments, &argStr); err2 == nil {
							if err3 := json.Unmarshal([]byte(argStr), &args); err3 != nil {
								log.Printf("Failed to parse get_image_analysis_guidance arguments after double unmarshal: %v", err3)
								aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error parsing image guidance arguments: " + err3.Error()})
								continue
							}
						} else {
							log.Printf("Failed to parse get_image_analysis_guidance arguments: %v", err)
							aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error parsing image guidance arguments: " + err.Error()})
							continue
						}
					}

					log.Printf("Parsed image guidance arguments: ImageType='%s', AnalysisRequest='%s'",
						args.ImageType, args.AnalysisRequest)

					result := getImageAnalysisGuidance(args.ImageType, args.AnalysisRequest)
					log.Printf("Image guidance result: %s", result)
					aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": result})
				} else if call.Function.Name == "get_workflow_step_instruction" {
					log.Printf("get_workflow_step_instruction called with arguments: %s", string(call.Function.Arguments))
					var args struct {
						CurrentStep     int    `json:"current_step"`
						UserMessage     string `json:"user_message,omitempty"`
						ImageAnalysis   string `json:"image_analysis,omitempty"`
						PreviousContext string `json:"previous_context,omitempty"`
					}

					if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
						var argStr string
						if err2 := json.Unmarshal(call.Function.Arguments, &argStr); err2 == nil {
							if err3 := json.Unmarshal([]byte(argStr), &args); err3 != nil {
								log.Printf("Failed to parse get_workflow_step_instruction arguments after double unmarshal: %v", err3)
								aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error parsing workflow step arguments: " + err3.Error()})
								continue
							}
						} else {
							log.Printf("Failed to parse get_workflow_step_instruction arguments: %v", err)
							aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error parsing workflow step arguments: " + err.Error()})
							continue
						}
					}

					log.Printf("Parsed workflow step arguments: CurrentStep=%d, UserMessage='%s', ImageAnalysis='%s', PreviousContext='%s'",
						args.CurrentStep, args.UserMessage, args.ImageAnalysis, args.PreviousContext)

					result := getWorkflowStepInstruction(args.CurrentStep, args.UserMessage, args.ImageAnalysis, args.PreviousContext)
					log.Printf("Workflow step result: %s", result)
					aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": result})
				} else if call.Function.Name == "get_current_workflow_step" {
					log.Printf("get_current_workflow_step called with arguments: %s", string(call.Function.Arguments))
					var args struct {
						UserMessage     string `json:"user_message"`
						ImageAnalysis   string `json:"image_analysis,omitempty"`
						PreviousContext string `json:"previous_context,omitempty"`
					}

					if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
						var argStr string
						if err2 := json.Unmarshal(call.Function.Arguments, &argStr); err2 == nil {
							if err3 := json.Unmarshal([]byte(argStr), &args); err3 != nil {
								log.Printf("Failed to parse get_current_workflow_step arguments after double unmarshal: %v", err3)
								aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error parsing current step arguments: " + err3.Error()})
								continue
							}
						} else {
							log.Printf("Failed to parse get_current_workflow_step arguments: %v", err)
							aggregatedOutputs = append(aggregatedOutputs, map[string]interface{}{"tool_call_id": call.ID, "output": "Error parsing current step arguments: " + err.Error()})
							continue
						}
					}

					log.Printf("Parsed current step arguments: UserMessage='%s', ImageAnalysis='%s', PreviousContext='%s'",
						args.UserMessage, args.ImageAnalysis, args.PreviousContext)

					stepNumber := getCurrentWorkflowStep(args.UserMessage, args.ImageAnalysis, args.PreviousContext)
					result := fmt.Sprintf("Current workflow step: %d", stepNumber)
					log.Printf("Current step result: %s", result)
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

// getWorkflowStepInstruction manages GPT workflow and provides step-by-step instructions
func getWorkflowStepInstruction(currentStep int, userMessage, imageAnalysis, previousContext string) string {
	log.Printf("getWorkflowStepInstruction called with: currentStep=%d, userMessage='%s', imageAnalysis='%s', previousContext='%s'",
		currentStep, userMessage, imageAnalysis, previousContext)

	var instruction strings.Builder

	// Persona - สั้นและกระชับ
	instruction.WriteString("🌟 **NCS Assistant** - เป็นมิตร มืออาชีพ กระชับแต่ครบถ้วน\n")
	instruction.WriteString("🎯 **เป้าหมาย:** นำลูกค้าจากทักทายถึงจองสำเร็จ - ตอบสั้น แต่ชัดเจน\n")
	instruction.WriteString("� **สไตล์:** เป็นมิตร + อีโมจิ + ไม่กดดัน + เน้นคุณภาพ\n\n")

	switch currentStep {
	case 1:
		// Step 1: Initial customer contact and consultation
		instruction.WriteString("🔄 **STEP 1: การปรึกษาและประเมินความต้องการ**\n\n")
		instruction.WriteString("**สิ่งที่คุณต้องทำ:**\n")
		instruction.WriteString("• ต้อนรับลูกค้าด้วยความเป็นมิตรและมืออาชีพ\n")
		instruction.WriteString("• หากมีรูปภาพ: วิเคราะห์และให้คำปรึกษาเชี่ยวชาญ\n")
		instruction.WriteString("• หากไม่มีรูปภาพ: สอบถามข้อมูลอย่างละเอียดและให้คำแนะนำ\n")
		instruction.WriteString("• ระบุประเภทและขนาดสิ่งของที่ต้องการทำความสะอาด\n")
		instruction.WriteString("• ประเมินสภาพและแนะนำบริการที่เหมาะสม\n")
		instruction.WriteString("• เรียกใช้ get_action_step_summary เมื่อได้ข้อมูลครบถ้วน\n\n")
		instruction.WriteString("**ห้ามทำ:**\n")
		instruction.WriteString("• ไม่บังคับให้ลูกค้าส่งรูปภาพ\n")
		instruction.WriteString("• ไม่ให้ราคาทันทีโดยไม่มีข้อมูลครบถ้วน\n")
		instruction.WriteString("• ไม่เรียกใช้ get_ncs_pricing ในขั้นตอนนี้\n\n")
		instruction.WriteString("**ตัวอย่าง (มีรูป):** \"เห็นเป็น[ประเภท][ขนาด] มี[ปัญหา] ให้เตรียมแผนดูแลให้นะคะ\"\n")
		instruction.WriteString("**ตัวอย่าง (ไม่มีรูป):** \"สวัสดีค่ะ! ขอทราบ: ประเภท/ขนาด/ปัญหาที่พบ เพื่อแนะนำบริการที่เหมาะสมค่ะ\"\n")
		instruction.WriteString("**Step ถัดไป:** เมื่อได้ข้อมูลครบ ให้เรียกใช้ getWorkflowStepInstruction(2, ...)")

	case 2:
		// Step 2: Premium service consultation and recommendation
		instruction.WriteString("🔄 **STEP 2: คำปรึกษาและแนะนำบริการระดับพรีเมียม**\n\n")
		instruction.WriteString("**สิ่งที่คุณต้องทำ:**\n")
		instruction.WriteString("• นำเสนอบริการที่เหมาะสมพร้อมอธิบายคุณประโยชน์\n")
		instruction.WriteString("• เน้นคุณภาพและมาตรฐานระดับพรีเมียม\n")
		instruction.WriteString("• สอบถามข้อมูลที่ขาดหายไปอย่างเป็นมิตร:\n")
		instruction.WriteString("  - ขนาดที่แน่นอนสำหรับการคิดราคา\n")
		instruction.WriteString("  - สถานะลูกค้า (ลูกค้าใหม่หรือสมาชิก VIP)\n")
		instruction.WriteString("  - ความสนใจในแพคเพจพิเศษ\n")
		instruction.WriteString("• ให้ความมั่นใจเรื่องคุณภาพและผลลัพธ์\n\n")
		instruction.WriteString("**ห้ามทำ:**\n")
		instruction.WriteString("• ไม่เรียกใช้ get_ncs_pricing จนกว่าจะได้ข้อมูลครบถ้วน\n")
		instruction.WriteString("• ไม่กดดันหรือรีบเร่งลูกค้า\n\n")
		instruction.WriteString("**ตัวอย่าง:** \"แนะนำ[บริการ]ระดับพรีเมียม ขอทราบ: 1)ขนาดแน่นอน 2)สมาชิก VIP? 3)สนใจแพคเพจ? เพื่อประเมินราคาให้ค่ะ\"\n")
		instruction.WriteString("**Step ถัดไป:** เมื่อได้ข้อมูลครบ ให้เรียกใช้ getWorkflowStepInstruction(3, ...)")

	case 3:
		// Step 3: Premium pricing presentation and value proposition
		instruction.WriteString("🔄 **STEP 3: นำเสนอราคาและคุณค่าของบริการ**\n\n")
		instruction.WriteString("**สิ่งที่คุณต้องทำ:**\n")
		instruction.WriteString("• เรียกใช้ get_ncs_pricing พร้อมข้อมูลครบถ้วน\n")
		instruction.WriteString("• นำเสนอราคาแบบโปร่งใสและมืออาชีพ\n")
		instruction.WriteString("• อธิบายคุณค่าและสิ่งที่ลูกค้าจะได้รับ\n")
		instruction.WriteString("• เน้นมาตรฐานคุณภาพและการรับประกัน\n")
		instruction.WriteString("• แนะนำส่วนลดหรือโปรโมชั่นที่เหมาะสม\n")
		instruction.WriteString("• ให้เวลาลูกค้าพิจารณาโดยไม่กดดัน\n\n")
		instruction.WriteString("**ห้ามทำ:**\n")
		instruction.WriteString("• ไม่เรียกใช้ get_available_slots_with_months ในขั้นตอนนี้\n")
		instruction.WriteString("• ไม่บังคับให้ตัดสินใจทันที\n\n")
		instruction.WriteString("**ตัวอย่าง:** \"ราคาสำหรับคุณ: [ผลจาก pricing] ✨รับประกัน 100% พร้อมบริการหลังขาย พอใจราคาสามารถเช็ควันว่างได้เลยค่ะ\"\n")
		instruction.WriteString("**Step ถัดไป:** เมื่อลูกค้าพอใจราคา ให้เรียกใช้ getWorkflowStepInstruction(4, ...)")

	case 4:
		// Step 4: Premium scheduling and booking experience
		instruction.WriteString("🔄 **STEP 4: การจองคิวแบบพรีเมียมและยืดหยุ่น**\n\n")
		instruction.WriteString("**สิ่งที่คุณต้องทำ:**\n")
		instruction.WriteString("• สอบถามเดือนที่ต้องการอย่างเป็นมิตร\n")
		instruction.WriteString("• เรียกใช้ get_available_slots_with_months\n")
		instruction.WriteString("• นำเสนอตัวเลือกวันเวลาที่หลากหลาย\n")
		instruction.WriteString("• เน้นความยืดหยุ่นและสะดวกสบาย\n")
		instruction.WriteString("• ยืนยันรายละเอียดการจองครบถ้วน\n")
		instruction.WriteString("• อธิบายขั้นตอนการจ่ายมัดจำอย่างชัดเจน\n\n")
		instruction.WriteString("**ห้ามทำ:**\n")
		instruction.WriteString("• ไม่ยืนยันการจองจนกว่าลูกค้าจะแน่ใจ\n")
		instruction.WriteString("• ไม่รีบเร่งในการเลือกวัน\n\n")
		instruction.WriteString("**ตัวอย่าง:** \"ดีค่ะ! สะดวกเดือนไหน? → เช็คตาราง → 📅วันว่าง[เดือน]: [ผลระบบ] *เปลี่ยนได้ล่วงหน้า 24ชม*\"\n")
		instruction.WriteString("**Step ถัดไป:** เมื่อเลือกวันเสร็จ ให้เรียกใช้ getWorkflowStepInstruction(5, ...)")

	case 5:
		// Step 5: Premium booking confirmation and VIP treatment
		instruction.WriteString("🔄 **STEP 5: การยืนยันการจองและบริการ VIP**\n\n")
		instruction.WriteString("**สิ่งที่คุณต้องทำ:**\n")
		instruction.WriteString("• สรุปการจองแบบมืออาชีพและครบถ้วน\n")
		instruction.WriteString("• ยืนยันวันเวลา ที่อยู่ และข้อมูลติดต่อ\n")
		instruction.WriteString("• แจ้งยอดมัดจำและช่องทางการชำระ\n")
		instruction.WriteString("• อธิบายขั้นตอนถัดไปอย่างชัดเจน\n")
		instruction.WriteString("• มอบความมั่นใจและการดูแลแบบ VIP\n\n")
		instruction.WriteString("**ตัวอย่าง:** \"🎉ยินดีต้อนรับ NCS! 📋สรุป: [บริการ] [วันเวลา] [ราคา] 💳มัดจำ[จำนวน] โอนแล้วส่งสลิปยืนยันค่ะ\"\n")
		instruction.WriteString("💳 มัดจำ: [จำนวนมัดจำ]\n\n")
		instruction.WriteString("🏆 **สิทธิพิเศษของคุณ:**\n")
		instruction.WriteString("• รับประกันความพึงพอใจ 100%\n")
		instruction.WriteString("• ทีมผู้เชี่ยวชาญมืออาชีพ\n")
		instruction.WriteString("• บริการหลังการขายฟรี\n")
		instruction.WriteString("• สิทธิ์สมาชิก VIP สำหรับครั้งต่อไป\n\n")
		instruction.WriteString("💡 **ขั้นตอนถัดไป:**\n")
		instruction.WriteString("1. ชำระมัดจำผ่าน [ช่องทางชำระ]\n")
		instruction.WriteString("2. ส่งสลิปการโอนมายืนยัน\n")
		instruction.WriteString("3. เราจะติดต่อยืนยันก่อนวันนัดหมาย 1 วัน\n\n")
		instruction.WriteString("ขอบคุณที่ไว้วางใจให้เราดูแลสิ่งสำคัญของคุณค่ะ เรามั่นใจว่าคุณจะประทับใจกับผลลัพธ์! 💫\"\n\n")
		instruction.WriteString("**Step ถัดไป:** รอการยืนยันชำระเงิน - กลับไป Step 1 สำหรับลูกค้าคนต่อไป")

	default:
		// Default: Redirect to appropriate step
		instruction.WriteString("🔄 **STEP MANAGEMENT: กำหนดขั้นตอนใหม่**\n\n")
		instruction.WriteString("**วิเคราะห์สถานการณ์:**\n")
		if strings.Contains(strings.ToLower(userMessage), "รูปภาพ") || strings.Contains(userMessage, "ภาพ") || imageAnalysis != "" {
			instruction.WriteString("• พบการส่งรูปภาพ → เรียกใช้ getWorkflowStepInstruction(1, ...)\n")
		} else if strings.Contains(strings.ToLower(userMessage), "ราคา") || strings.Contains(userMessage, "เท่าไหร่") {
			instruction.WriteString("• สอบถามราคา → เรียกใช้ getWorkflowStepInstruction(2, ...)\n")
		} else if strings.Contains(strings.ToLower(userMessage), "จอง") || strings.Contains(userMessage, "คิว") {
			instruction.WriteString("• ต้องการจอง → เรียกใช้ getWorkflowStepInstruction(4, ...)\n")
		} else {
			instruction.WriteString("• ทักทายทั่วไป → เรียกใช้ getWorkflowStepInstruction(1, ...)\n")
		}
		instruction.WriteString("\n**กรุณาเรียกใช้ getWorkflowStepInstruction ใหม่ด้วยขั้นตอนที่ถูกต้อง**")
	}

	return instruction.String()
}

// getCurrentWorkflowStep analyzes user message and context to determine current step
func getCurrentWorkflowStep(userMessage, imageAnalysis, previousContext string) int {
	log.Printf("getCurrentWorkflowStep called with: userMessage='%s', imageAnalysis='%s', previousContext='%s'",
		userMessage, imageAnalysis, previousContext)

	// Step 1: Image analysis or initial contact
	if imageAnalysis != "" || strings.Contains(strings.ToLower(userMessage), "รูปภาพ") || strings.Contains(userMessage, "ภาพ") {
		return 1
	}

	// Step 2: Service inquiry after image analysis
	if strings.Contains(strings.ToLower(previousContext), "step 1") &&
		(strings.Contains(strings.ToLower(userMessage), "บริการ") ||
			strings.Contains(userMessage, "ขนาด") ||
			strings.Contains(userMessage, "ต้องการ")) {
		return 2
	}

	// Step 3: Price inquiry
	if strings.Contains(strings.ToLower(userMessage), "ราคา") ||
		strings.Contains(userMessage, "เท่าไหร่") ||
		strings.Contains(userMessage, "ค่าใช้จ่าย") {
		return 3
	}

	// Step 4: Booking inquiry
	if strings.Contains(strings.ToLower(userMessage), "จอง") ||
		strings.Contains(userMessage, "คิว") ||
		strings.Contains(userMessage, "วันไหน") ||
		strings.Contains(userMessage, "ว่าง") {
		return 4
	}

	// Step 5: Confirmation
	if strings.Contains(strings.ToLower(userMessage), "ยืนยัน") ||
		strings.Contains(userMessage, "ตกลง") ||
		strings.Contains(userMessage, "ชำระ") {
		return 5
	}

	// Default to step 1 for new conversations
	return 1
}

// getActionStepSummary provides step-by-step guidance before taking action based on image analysis
func getActionStepSummary(analysisType, itemIdentified, conditionAssessed, recommendedService string) string {
	log.Printf("getActionStepSummary called with: analysisType='%s', itemIdentified='%s', conditionAssessed='%s', recommendedService='%s'",
		analysisType, itemIdentified, conditionAssessed, recommendedService)

	// Validate inputs
	if analysisType == "" || itemIdentified == "" {
		return "ข้อมูลไม่ครบถ้วน กรุณาระบุประเภทการวิเคราะห์และสิ่งที่ตรวจพบ"
	}

	var stepSummary strings.Builder
	stepSummary.WriteString("📋 **สรุปขั้นตอนการดำเนินการ**\n\n")

	// Step 1: Analysis confirmation
	stepSummary.WriteString("🔍 **ขั้นตอนที่ 1: ยืนยันการวิเคราะห์**\n")
	stepSummary.WriteString(fmt.Sprintf("• วิเคราะห์รูปภาพ: %s\n", analysisType))
	stepSummary.WriteString(fmt.Sprintf("• สิ่งที่ตรวจพบ: %s\n", itemIdentified))
	if conditionAssessed != "" {
		stepSummary.WriteString(fmt.Sprintf("• สภาพที่ประเมิน: %s\n", conditionAssessed))
	}
	stepSummary.WriteString("\n")

	// Step 2: Service recommendation
	stepSummary.WriteString("💡 **ขั้นตอนที่ 2: คำแนะนำบริการ**\n")
	if recommendedService != "" {
		stepSummary.WriteString(fmt.Sprintf("• บริการที่แนะนำ: %s\n", recommendedService))

		// Add specific guidance based on service type
		switch strings.ToLower(recommendedService) {
		case "disinfection", "กำจัดเชื้อโรค":
			stepSummary.WriteString("• เหมาะสำหรับ: กำจัดเชื้อโรค ไรฝุ่น และแบคทีเรีย\n")
			stepSummary.WriteString("• ระยะเวลา: ประมาณ 2-3 ชั่วโมง\n")
		case "washing", "ซักขจัดคราบ":
			stepSummary.WriteString("• เหมาะสำหรับ: ขจัดคราบสกปรก กลิ่น และฟื้นฟูผ้า\n")
			stepSummary.WriteString("• ระยะเวลา: ประมาณ 4-6 ชั่วโมง\n")
		case "both", "ทั้งสองบริการ":
			stepSummary.WriteString("• บริการครบวงจร: กำจัดเชื้อโรค + ซักขจัดคราบ\n")
			stepSummary.WriteString("• ระยะเวลา: ประมาณ 6-8 ชั่วโมง\n")
		}
	} else {
		stepSummary.WriteString("• กรุณาระบุบริการที่ต้องการ\n")
	}
	stepSummary.WriteString("\n")

	// Step 3: Next actions
	stepSummary.WriteString("📞 **ขั้นตอนที่ 3: การดำเนินการต่อไป**\n")
	stepSummary.WriteString("• สอบถามราคาและรายละเอียดเพิ่มเติม\n")
	stepSummary.WriteString("• เลือกวันเวลาที่สะดวก\n")
	stepSummary.WriteString("• ยืนยันการจองและชำระมัดจำ\n")
	stepSummary.WriteString("\n")

	// Additional recommendations
	stepSummary.WriteString("💭 **คำแนะนำเพิ่มเติม**\n")
	if strings.Contains(strings.ToLower(itemIdentified), "mattress") || strings.Contains(itemIdentified, "ที่นอน") {
		stepSummary.WriteString("• ควรทำความสะอาดที่นอนทุก 6-12 เดือน\n")
		stepSummary.WriteString("• หากมีปัญหาไรฝุ่น แนะนำบริการกำจัดเชื้อโรค\n")
	} else if strings.Contains(strings.ToLower(itemIdentified), "sofa") || strings.Contains(itemIdentified, "โซฟา") {
		stepSummary.WriteString("• ควรทำความสะอาดโซฟาทุก 6-9 เดือน\n")
		stepSummary.WriteString("• หากมีคราบสกปรก แนะนำบริการซักขจัดคราบ\n")
	} else if strings.Contains(strings.ToLower(itemIdentified), "curtain") || strings.Contains(itemIdentified, "ม่าน") {
		stepSummary.WriteString("• ควรทำความสะอาดม่านทุก 3-6 เดือน\n")
		stepSummary.WriteString("• หากเป็นผ้าบาง ใช้บริการซักขจัดคราบ\n")
	}

	stepSummary.WriteString("• หากมีข้อสงสัย กรุณาสอบถามเจ้าหน้าที่\n")
	stepSummary.WriteString("• สามารถขอดูผลงานก่อนหน้าได้\n\n")

	stepSummary.WriteString("❓ **ต้องการดำเนินการขั้นตอนไหนต่อไป?**")

	return stepSummary.String()
}

// getImageAnalysisGuidance provides guidance for image analysis process
func getImageAnalysisGuidance(imageType, analysisRequest string) string {
	log.Printf("getImageAnalysisGuidance called with: imageType='%s', analysisRequest='%s'",
		imageType, analysisRequest)

	var guidance strings.Builder
	guidance.WriteString("🔍 **แนวทางการวิเคราะห์รูปภาพ**\n\n")

	// Analysis checklist
	guidance.WriteString("📝 **รายการตรวจสอบ**\n")
	guidance.WriteString("• ประเภทสิ่งของ: (ที่นอน/โซฟา/ม่าน/พรม)\n")
	guidance.WriteString("• ขนาดโดยประมาณ: (3ฟุต/6ฟุต/2ที่นั่ง ฯลฯ)\n")
	guidance.WriteString("• สภาพปัจจุบัน: (สะอาด/สกปรก/มีคราบ/มีกลิ่น)\n")
	guidance.WriteString("• ปัญหาที่พบ: (ไรฝุ่น/คราบ/กลิ่น/เชื้อโรค)\n")
	guidance.WriteString("• ความเร่งด่วน: (ปกติ/เร่งด่วน)\n\n")

	// Recommended analysis approach
	guidance.WriteString("🎯 **วิธีการวิเคราะห์**\n")
	if strings.Contains(strings.ToLower(imageType), "mattress") || strings.Contains(imageType, "ที่นอน") {
		guidance.WriteString("• ตรวจสอบคราบเหลือง (เหงื่อ/ปัสสาวะ)\n")
		guidance.WriteString("• ดูรอยดำ (เชื้อรา/ความชื้น)\n")
		guidance.WriteString("• ประเมินอายุการใช้งาน\n")
	} else if strings.Contains(strings.ToLower(imageType), "sofa") || strings.Contains(imageType, "โซฟา") {
		guidance.WriteString("• ตรวจสอบผ้าหุ้ม (ผ้า/หนัง/หนังเทียม)\n")
		guidance.WriteString("• ดูคราบอาหาร/เครื่องดื่ม\n")
		guidance.WriteString("• ประเมินความสึกหรอ\n")
	} else if strings.Contains(strings.ToLower(imageType), "curtain") || strings.Contains(imageType, "ม่าน") {
		guidance.WriteString("• ตรวจสอบฝุ่นและคราบ\n")
		guidance.WriteString("• ดูความหนาของผ้า\n")
		guidance.WriteString("• ประเมินวิธีการซัก\n")
	}

	guidance.WriteString("\n💡 **คำแนะนำบริการ**\n")
	guidance.WriteString("• หากมีไรฝุ่น/เชื้อโรค → บริการกำจัดเชื้อโรค\n")
	guidance.WriteString("• หากมีคราบ/กลิ่น → บริการซักขจัดคราบ\n")
	guidance.WriteString("• หากมีทั้งสองปัญหา → บริการครบวงจร\n\n")

	guidance.WriteString("📞 **ขั้นตอนต่อไป**\n")
	guidance.WriteString("• วิเคราะห์รูปภาพตามแนวทาง\n")
	guidance.WriteString("• แนะนำบริการที่เหมาะสม\n")
	guidance.WriteString("• เรียกใช้ get_action_step_summary\n")
	guidance.WriteString("• ดำเนินการตามขั้นตอน")

	return guidance.String()
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

// detectHumanRequest returns true when the message signals a request for a human agent
func detectHumanRequest(msg string) bool {
	lower := strings.ToLower(msg)
	keywords := []string{
		"ขอคุยกับคน", "อยากคุยกับคน", "ต้องการคุยกับคน",
		"ขอพนักงาน", "อยากคุยกับพนักงาน", "คุยกับพนักงาน",
		"ขอเจ้าหน้าที่", "อยากคุยกับเจ้าหน้าที่",
		"คุยกับคนได้ไหม", "มีคนตอบไหม", "ขอให้คนตอบ",
		"คนจริงๆ", "ไม่ใช่บอท", "ไม่ใช่ai",
		"human agent", "speak to human", "talk to human",
		"real person",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// pushLineMessage sends a push message to a LINE user via the Push API
func pushLineMessage(userId, message string) error {
	channelToken := os.Getenv("LINE_CHANNEL_ACCESS_TOKEN")
	if channelToken == "" {
		return fmt.Errorf("LINE channel access token not set")
	}
	payload := map[string]interface{}{
		"to": userId,
		"messages": []map[string]string{{
			"type": "text",
			"text": message,
		}},
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal push payload: %w", err)
	}
	client := &http.Client{}
	req, err := http.NewRequest("POST", "https://api.line.me/v2/bot/message/push", bytes.NewReader(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create push request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+channelToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send push message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LINE push error (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// --- Conversation Admin API Handlers ---

// ConversationSummary is a lightweight view of a conversation for the list page
type ConversationSummary struct {
	UserID       string `json:"user_id"`
	LastMessage  string `json:"last_message"`
	LastSeen     string `json:"last_seen"`
	Takeover     bool   `json:"takeover"`
	WantsHuman   bool   `json:"wants_human"`
	MessageCount int    `json:"message_count"`
}

func handleGetConversations(c *fiber.Ctx) error {
	userThreadLock.Lock()
	defer userThreadLock.Unlock()

	summaries := make([]ConversationSummary, 0, len(userConversations))
	for _, conv := range userConversations {
		lastMsg := ""
		if len(conv.Messages) > 0 {
			lastMsg = conv.Messages[len(conv.Messages)-1].Text
			if len(lastMsg) > 80 {
				lastMsg = lastMsg[:80] + "…"
			}
		}
		summaries = append(summaries, ConversationSummary{
			UserID:       conv.UserID,
			LastMessage:  lastMsg,
			LastSeen:     conv.LastSeen,
			Takeover:     conv.Takeover,
			WantsHuman:   conv.WantsHuman,
			MessageCount: len(conv.Messages),
		})
	}
	return c.JSON(summaries)
}

func handleGetConversationMessages(c *fiber.Ctx) error {
	userId := c.Params("userId")
	if userId == "" {
		return respondError(c, fiber.StatusBadRequest, "userId is required")
	}

	userThreadLock.Lock()
	conv, ok := userConversations[userId]
	var result UserConversation
	if ok {
		result = *conv // shallow copy is fine for read
	}
	userThreadLock.Unlock()

	if !ok {
		return respondError(c, fiber.StatusNotFound, "conversation not found")
	}
	return c.JSON(result)
}

func handleTakeoverConversation(c *fiber.Ctx) error {
	userId := c.Params("userId")
	if userId == "" {
		return respondError(c, fiber.StatusBadRequest, "userId is required")
	}

	userThreadLock.Lock()
	if _, ok := userConversations[userId]; !ok {
		userConversations[userId] = &UserConversation{UserID: userId}
	}
	userConversations[userId].Takeover = true
	userThreadLock.Unlock()

	log.Printf("Admin took over conversation for user %s", userId)
	return c.JSON(fiber.Map{"status": "ok", "takeover": true})
}

func handleReleaseConversation(c *fiber.Ctx) error {
	userId := c.Params("userId")
	if userId == "" {
		return respondError(c, fiber.StatusBadRequest, "userId is required")
	}

	userThreadLock.Lock()
	if conv, ok := userConversations[userId]; ok {
		conv.Takeover = false
		conv.WantsHuman = false
	}
	userThreadLock.Unlock()

	log.Printf("Admin released conversation for user %s - AI resumed", userId)
	return c.JSON(fiber.Map{"status": "ok", "takeover": false})
}

type AdminReplyRequest struct {
	Message string `json:"message"`
}

func handleAdminReply(c *fiber.Ctx) error {
	userId := c.Params("userId")
	if userId == "" {
		return respondError(c, fiber.StatusBadRequest, "userId is required")
	}

	var req AdminReplyRequest
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		return respondError(c, fiber.StatusBadRequest, "message is required")
	}

	if err := pushLineMessage(userId, req.Message); err != nil {
		log.Printf("Failed to push LINE message to %s: %v", userId, err)
		return respondError(c, fiber.StatusInternalServerError, "failed to send LINE message: "+err.Error())
	}

	// Record admin message in history
	userThreadLock.Lock()
	if _, ok := userConversations[userId]; !ok {
		userConversations[userId] = &UserConversation{UserID: userId}
	}
	userConversations[userId].appendMessage("admin", req.Message)
	userThreadLock.Unlock()

	log.Printf("Admin replied to user %s: %s", userId, req.Message)
	return c.JSON(fiber.Map{"status": "ok"})
}
