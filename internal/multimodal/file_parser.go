package multimodal

import (
	"github.com/ledongthuc/pdf"
	"io"
	"os"
	"strings"
)

func ParseFile(filePath string) (string, error) {
	if strings.HasSuffix(strings.ToLower(filePath), ".pdf") {
		f, r, err := pdf.Open(filePath)
		if err != nil {
			return "", err
		}
		defer f.Close()
		reader, err := r.GetPlainText()
		if err != nil {
			return "", err
		}
		b, err := io.ReadAll(reader)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	if strings.HasSuffix(strings.ToLower(filePath), ".txt") {
		b, err := os.ReadFile(filePath)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	if strings.HasSuffix(strings.ToLower(filePath), ".md") {
		b, err := os.ReadFile(filePath)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}

	return "", nil
}
