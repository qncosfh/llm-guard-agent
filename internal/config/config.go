package config

import (
	"gopkg.in/yaml.v3"
	"os"
)

type AppConfig struct {
	Listen    string `yaml:"listen"`
	ModelURL  string `yaml:"model_url"`
	ModelName string `yaml:"model_name"`
	ApiKey    string `yaml:"api_key"`
	RulesFile string `yaml:"rules_file"`
}

var Cfg AppConfig

func LoadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &Cfg)
}
