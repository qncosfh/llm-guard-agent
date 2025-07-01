package rules

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"regexp"
	"strings"
)

var debug = true // 可通过配置控制

type Rule struct {
	Type        string `yaml:"type"` // input / output / filename
	Keyword     string `yaml:"keyword"`
	Description string `yaml:"description"`
}

type RuleList struct {
	Rules []Rule `yaml:"rules"`
}

var allRules []Rule

func AllRules() []Rule {
	return allRules
}

// 加载规则
func LoadFromYAML(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var list RuleList
	if err := yaml.Unmarshal(data, &list); err != nil {
		return err
	}

	allRules = list.Rules
	if debug {
		fmt.Printf("[规则加载] 加载规则%d条\n", len(allRules))
	}
	return nil
}

// 匹配规则
func Match(text, typ string) (string, bool) {
	lowerText := strings.ToLower(text)

	for _, r := range allRules {
		if r.Type != typ {
			continue
		}

		kw := r.Keyword

		if debug {
			//fmt.Printf("[规则检测]......")
		}

		// 判断是否为正则（假设规则用斜杠包裹，例如 /xxx/）
		if len(kw) > 2 && kw[0] == '/' && kw[len(kw)-1] == '/' {
			pattern := kw[1 : len(kw)-1]
			matched, err := regexp.MatchString(pattern, text)
			if err == nil && matched {
				if debug {
					fmt.Printf("[命中正则规则] 规则:%s 内容:%s\n", kw, text)
				}
				return kw, true
			}
		} else {
			// 忽略大小写包含匹配
			if strings.Contains(lowerText, strings.ToLower(kw)) {
				if debug {
					fmt.Printf("[命中字符串规则] 规则:%s 内容:%s\n", kw, text)
				}
				return kw, true
			}
		}
	}
	return "", false
}

// 根据类型和关键词查找规则的description
func GetDescription(typ, keyword string) string {
	for _, r := range allRules {
		if r.Type == typ && r.Keyword == keyword {
			return r.Description
		}
	}
	return ""
}

// 检测整段文本，返回所有命中关键词和描述（去重）
func MatchAll(text, typ string) ([]string, []string) {
	lowerText := strings.ToLower(text)
	found := make(map[string]struct{})
	var keywords []string
	var descs []string
	for _, r := range allRules {
		if r.Type != typ {
			continue
		}
		kw := r.Keyword
		if len(kw) > 2 && kw[0] == '/' && kw[len(kw)-1] == '/' {
			pattern := kw[1 : len(kw)-1]
			matched, err := regexp.MatchString(pattern, text)
			if err == nil && matched {
				if _, ok := found[kw]; !ok {
					keywords = append(keywords, kw)
					descs = append(descs, r.Description)
					found[kw] = struct{}{}
				}
			}
		} else {
			if strings.Contains(lowerText, strings.ToLower(kw)) {
				if _, ok := found[kw]; !ok {
					keywords = append(keywords, kw)
					descs = append(descs, r.Description)
					found[kw] = struct{}{}
				}
			}
		}
	}
	return keywords, descs
}

// 滑动窗口检测，窗口长度 window，步长 step，返回所有命中的关键词和描述（全局去重）
func MatchAllSlidingWindow(text string, typ string, window int, step int) ([]string, []string) {
	runes := []rune(text)
	found := make(map[string]struct{})
	var keywords []string
	var descs []string
	if window > len(runes) {
		window = len(runes)
	}
	for i := 0; i <= len(runes)-window; i += step {
		slice := string(runes[i : i+window])
		ks, ds := MatchAll(slice, typ)
		for idx, kw := range ks {
			if _, ok := found[kw]; !ok {
				keywords = append(keywords, kw)
				descs = append(descs, ds[idx])
				found[kw] = struct{}{}
			}
		}
	}
	return keywords, descs
}
