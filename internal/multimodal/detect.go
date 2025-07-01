package multimodal

import (
	"errors"
	"path/filepath"
	"strings"
)

// 检查文件名是否包含敏感关键词
func CheckFilename(name string, matchFunc func(text, t string) (string, bool)) (string, bool) {
	return matchFunc(name, "filename")
}

// 模拟 OCR 识别图片内容（你后续可接入 PaddleOCR、Tesseract 等）
func OCRImage(filePath string) (string, error) {
	if !strings.HasSuffix(filePath, ".png") && !strings.HasSuffix(filePath, ".jpg") {
		return "", errors.New("不支持的图片格式")
	}
	// TODO: 实际实现
	return "模拟图片中的文字内容", nil
}

// 模拟语音转文字（你可对接 whisper/cwhisper）
func SpeechToText(filePath string) (string, error) {
	if !strings.HasSuffix(filePath, ".wav") && !strings.HasSuffix(filePath, ".mp3") {
		return "", errors.New("不支持的音频格式")
	}
	// TODO: 实际实现
	return "模拟语音转写内容", nil
}

// 模拟文件解析（.pdf .doc .md .txt）
func ExtractTextFromFile(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".txt", ".md":
		return "模拟文件内容", nil
	case ".pdf":
		return "模拟 PDF 内容", nil
	case ".doc", ".docx":
		return "模拟 Word 内容", nil
	default:
		return "", errors.New("不支持的文件类型")
	}
}
