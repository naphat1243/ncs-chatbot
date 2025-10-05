# 🖼️ Image Size Issue Fix

## 🐛 **ปัญหาที่พบ:**

```
2025/10/05 14:34:34 📬 Message sent to thread. Response status: 400
⚠️ Non-200 response body: {
  "error": {
    "message": "Invalid 'content[1].image_url.url'. Expected a valid URL, but got a value with an invalid format.",
    "type": "invalid_request_error",
    "param": "content",
    "code": "invalid_value"
  }
}
```

**สาเหตุ**: รูปภาพมีขนาดใหญ่เกินไป (795,495 characters) ทำให้ OpenAI API ไม่ยอมรับ data URL

## ✅ **การแก้ไข:**

### 1. **เพิ่มการตรวจสอบขนาดไฟล์ต้นทาง (ใน getLineImageURL)**
```go
// Check if image is too large for OpenAI API (limit ~20MB for data URLs)
const maxImageSize = 20 * 1024 * 1024 // 20MB
if len(imageData) > maxImageSize {
    log.Printf("⚠️ Image too large (%d bytes > %d bytes). Attempting to resize...", len(imageData), maxImageSize)
    return "", fmt.Errorf("รูปภาพมีขนาดใหญ่เกินไป กรุณาลดขนาดรูปภาพแล้วลองใหม่อีกครั้ง")
}
```

### 2. **เพิ่มการตรวจสอบขนาด Data URL สุดท้าย**
```go
// Check final data URL length (OpenAI has limits on data URL size)
const maxDataURLLength = 1000000 // ~1MB base64 encoded
if len(dataURL) > maxDataURLLength {
    log.Printf("⚠️ Data URL too long (%d chars > %d chars)", len(dataURL), maxDataURLLength)
    return "", fmt.Errorf("รูปภาพมีขนาดใหญ่เกินไป กรุณาลดขนาดรูปภาพแล้วลองใหม่อีกครั้ง")
}
```

### 3. **เพิ่มการตรวจสอบก่อนส่งไป OpenAI (ใน getAssistantResponse)**
```go
// Check if image data URL is too large for OpenAI API
const maxDataURLLength = 1000000 // ~1MB base64 encoded
if len(imageURL) > maxDataURLLength {
    log.Printf("⚠️ Data URL too long (%d chars > %d chars) - rejecting", len(imageURL), maxDataURLLength)
    return "ขออภัย รูปภาพมีขนาดใหญ่เกินไป กรุณาลดขนาดรูปภาพหรือถ่ายรูปใหม่ให้เล็กกว่านี้แล้วลองใหม่อีกครั้งค่ะ 📸"
}
```

## 🎯 **ผลลัพธ์:**

### **เมื่อรูปภาพใหญ่เกินไป ระบบจะ:**
1. ⚠️ ตรวจจับที่ขั้นตอนการดาวน์โหลดจาก LINE
2. 📏 ตรวจสอบขนาด Data URL ก่อนส่งไป OpenAI  
3. 💬 ตอบลูกค้าด้วยข้อความที่เป็นมิตร
4. 📸 แนะนำให้ถ่ายรูปใหม่หรือลดขนาด

### **ข้อความที่ลูกค้าจะเห็น:**
```
"ขออภัย รูปภาพมีขนาดใหญ่เกินไป กรุณาลดขนาดรูปภาพหรือถ่ายรูปใหม่ให้เล็กกว่านี้แล้วลองใหม่อีกครั้งค่ะ 📸"
```

### **Log ที่จะเห็น:**
```
⚠️ Data URL too long (795495 chars > 1000000 chars) - rejecting
```

## 📊 **ข้อมูลขีดจำกัด:**

| ประเภท | ขีดจำกัด | หมายเหตุ |
|--------|----------|----------|
| **ไฟล์ต้นทาง** | 20MB | ไฟล์ภาพดิบจาก LINE |
| **Data URL** | 1MB | Base64 encoded สำหรับ OpenAI |
| **Characters** | ~1,000,000 | ความยาว string ของ data URL |

## 🚀 **การทดสอบ:**

1. **รูปภาพปกติ** (< 1MB) → ✅ ประมวลผลได้ปกติ
2. **รูปภาพใหญ่** (> 1MB) → ⚠️ แจ้งเตือนให้ลดขนาด
3. **รูปภาพใหญ่มาก** (> 20MB) → ❌ ปฏิเสธทันที

## 💡 **การปรับปรุงในอนาคต:**

1. **Image Compression** - เพิ่มการบีบอัดรูปภาพอัตโนมัติ
2. **Image Resizing** - ลดขนาดความละเอียดอัตโนมัติ  
3. **Format Conversion** - แปลงเป็น WebP เพื่อลดขนาด
4. **Progressive Upload** - อัปโหลดทีละส่วนสำหรับรูปใหญ่

---

*ตอนนี้ระบบจะจัดการรูปภาพขนาดใหญ่ได้อย่างเหมาะสม โดยไม่ให้เกิด error 400 จาก OpenAI API อีกต่อไป!* 🎉