package main

import (
	"encoding/json"
	"strings"
)

// FilterAdContent 从响应 content 中过滤广告内容（按行匹配关键词并移除）
func FilterAdContent(content string, keywords []string) string {
	if len(keywords) == 0 {
		return content
	}

	lines := strings.Split(content, "\n")
	var filtered []string
	for _, line := range lines {
		if containsAnyKeyword(line, keywords) {
			continue
		}
		filtered = append(filtered, line)
	}

	result := strings.Join(filtered, "\n")
	result = strings.TrimRight(result, "\n ")
	return result
}

// FilterNonStreamResponse 过滤非流式响应体中的广告
func FilterNonStreamResponse(body []byte, keywords []string) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return body, err
	}

	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return body, nil
	}

	modified := false
	for _, choice := range choices {
		choiceMap, ok := choice.(map[string]interface{})
		if !ok {
			continue
		}
		message, ok := choiceMap["message"].(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := message["content"].(string)
		if !ok {
			continue
		}
		filteredContent := FilterAdContent(content, keywords)
		if filteredContent != content {
			message["content"] = filteredContent
			modified = true
		}
	}

	if !modified {
		return body, nil
	}

	return json.Marshal(resp)
}

// containsAnyKeyword 检查文本是否包含任意一个关键词（大小写不敏感）
func containsAnyKeyword(text string, keywords []string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}
