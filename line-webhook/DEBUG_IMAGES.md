# 🔍 IMAGE DEBUGGING GUIDE

## Why Images Might Not Be Sent to GPT

I've added enhanced debugging to your application. Here are the most common reasons why images don't reach GPT:

### 🚨 **Most Likely Issues:**

### 1. **Missing LINE_CHANNEL_ACCESS_TOKEN**
**Check**: Run your app and look for this log:
```
ERROR: LINE_CHANNEL_ACCESS_TOKEN not set
```
**Fix**: Set your LINE Channel Access Token environment variable

### 2. **LINE API Image Download Fails**
**Check**: Look for these logs:
```
ERROR: Failed to download image: [error message]
Image download response status: 403 Forbidden
```
**Fix**: Verify your LINE Channel Access Token is correct and has proper permissions

### 3. **GPT Assistant Model Not Supporting Vision**
**Check**: Your OpenAI Assistant must be using `gpt-4-vision-preview` or `gpt-4o` model
**Fix**: Update your assistant model in OpenAI dashboard

### 4. **Message Format Detection Issue**
**Check**: Look for this log:
```
Detected image message, preparing vision request
```
**Fix**: If not appearing, the message format detection is failing

---

## 🔧 **How to Debug:**

### **Step 1: Check Environment Variables**
```bash
echo $LINE_CHANNEL_ACCESS_TOKEN
echo $CHATGPT_API_KEY
echo $OPENAI_ASSISTANT_ID
```

### **Step 2: Test Image Processing**
1. Start your application: `./line-webhook`
2. Send an image via LINE
3. Check logs for:
   - `Processing image message with ID: [message_id]`
   - `Starting image download for message ID: [message_id]`
   - `LINE_CHANNEL_ACCESS_TOKEN found: [token]...`
   - `Image data size: [bytes] bytes`
   - `Successfully created data URL. Length: [length] characters`
   - `Detected image message, preparing vision request`
   - `Vision request prepared successfully`

### **Step 3: Check GPT Assistant Configuration**
1. Go to OpenAI Dashboard → Assistants
2. Verify your assistant uses `gpt-4o` or `gpt-4-vision-preview`
3. Check if Vision capabilities are enabled

---

## 📋 **Debug Log Examples:**

### ✅ **Success Logs:**
```
Processing image message with ID: 12345
Starting image download for message ID: 12345
LINE_CHANNEL_ACCESS_TOKEN found: 1234567890...
Requesting image from: https://api-data.line.me/v2/bot/message/12345/content
Image download response status: 200 OK
Image data size: 15024 bytes
Image content type: image/jpeg
Successfully created data URL. Length: 20032 characters
Detected image message, preparing vision request
Image URL length: 20032 characters
Vision request prepared successfully
```

### ❌ **Error Logs:**
```
ERROR: LINE_CHANNEL_ACCESS_TOKEN not set
ERROR: Failed to download image: Get https://api-data.line.me/...: 403 Forbidden
ERROR: Image download failed. Response body: {"message":"Invalid signature"}
```

---

## 🛠️ **Quick Fixes:**

### **Issue**: LINE_CHANNEL_ACCESS_TOKEN not set
**Solution**:
```bash
export LINE_CHANNEL_ACCESS_TOKEN="your_token_here"
./line-webhook
```

### **Issue**: 403 Forbidden from LINE API
**Solution**: Check your LINE Channel settings:
1. LINE Developers Console
2. Verify Channel Access Token
3. Check webhook URL settings
4. Ensure Message API is enabled

### **Issue**: GPT doesn't analyze images
**Solution**: Update your OpenAI Assistant:
1. Model: `gpt-4o` (recommended) or `gpt-4-vision-preview`
2. Enable Vision capabilities
3. Add the function definitions I provided

---

## 🎯 **Test Command:**
After starting your app, send an image via LINE and check for ALL these logs in sequence:
1. `Processing image message with ID:`
2. `Starting image download for message ID:`
3. `Successfully created data URL`
4. `Detected image message, preparing vision request`
5. `Vision request prepared successfully`

If any of these are missing, that's where the problem is! 🕵️‍♂️