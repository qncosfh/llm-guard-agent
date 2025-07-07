package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	Listen    string `yaml:"Listen"`
	WebListen string `yaml:"WebListen"`
	ModelURL  string `yaml:"model_url"`
	ModelName string `yaml:"model_name"`
	ApiKey    string `yaml:"api_key"`
	RulesFile string `yaml:"rules_file"`
	Platform  string `yaml:"platform"`
	OnnxURL   string `yaml:"onnx_url"`
}

var Cfg AppConfig

func LoadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &Cfg)
}
