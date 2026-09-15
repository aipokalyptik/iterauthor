package model

import (
	"fmt"
	"slices"

	"github.com/aipokalyptik/iterauthor/internal/project"
)

func ApplyReasoning(payload map[string]any, m project.Model) error {
	value := m.Reasoning
	if value == "" || value == "default" {
		return nil
	}
	if err := (project.Inference{Reasoning: value}).Validate(); err != nil {
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
	if len(m.ReasoningOptions) > 0 && !slices.ContainsFunc(m.ReasoningOptions, func(s string) bool { return normalize(s) == normalize(value) }) {
		return fmt.Errorf("%s does not report support for reasoning %s; choose a supported option in Model settings", m.Name, value)
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
