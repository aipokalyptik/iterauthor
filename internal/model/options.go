package model

import (
	"github.com/aipokalyptik/iterauthor/internal/project"
)

func ApplyReasoning(payload map[string]any, m project.Model) error {
	value := m.Reasoning
	if value == "" || value == "default" {
		return nil
	}
	if err := m.ValidateReasoning(value); err != nil {
		return err
	}

	normalize := func(s string) string {
		if s == "off" {
			return "none"
		}
		if s == "on" {
			return "medium"
		}
		return s
	}

	if m.ReasoningField == "chat_template_kwargs" || (m.ReasoningField == "" && m.Provider == "llama.cpp") {
		kwargs := map[string]any{}
		switch value {
		case "none", "off":
			kwargs["enable_thinking"] = false
		case "on":
			kwargs["enable_thinking"] = true
		default:
			kwargs["enable_thinking"] = true
			kwargs["reasoning_effort"] = value
		}
		payload["chat_template_kwargs"] = kwargs
	} else {
		payload["reasoning_effort"] = normalize(value)
	}
	return nil
}
