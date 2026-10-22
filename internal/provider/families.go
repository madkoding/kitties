package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// --- chat completions (OpenAI-compatible) ---------------------------------------------------

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		Prompt     int `json:"prompt_tokens"`
		Completion int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *HTTPClient) callChat(ctx context.Context, messages []Message) (Reply, error) {
	body := map[string]any{
		"model":       c.opts.Model,
		"messages":    messages,
		"max_tokens":  c.opts.MaxTokens,
		"temperature": c.temperature(0.7),
	}
	data, err := c.post(ctx, "/chat/completions", body)
	if err != nil {
		return Reply{}, err
	}
	var resp chatResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return Reply{}, fmt.Errorf("unreadable chat response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return Reply{}, &HTTPError{Code: 400, Body: resp.Error.Message}
	}
	if len(resp.Choices) == 0 {
		return Reply{}, errors.New("the provider returned no choices")
	}
	reply := Reply{Content: strings.TrimSpace(resp.Choices[0].Message.Content)}
	if resp.Usage != nil {
		reply.Usage = Usage{Input: resp.Usage.Prompt, Output: resp.Usage.Completion}
	}
	if reply.Content == "" {
		return Reply{}, fmt.Errorf("the provider returned an empty answer (finish_reason=%q)", resp.Choices[0].FinishReason)
	}
	return reply, nil
}

// --- responses (codex) ---------------------------------------------------------------------

type responsesResponse struct {
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage *struct {
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *HTTPClient) callResponses(ctx context.Context, messages []Message) (Reply, error) {
	system, rest := splitSystem(messages)
	input := make([]map[string]any, 0, len(rest))
	for _, m := range rest {
		input = append(input, map[string]any{"role": m.Role, "content": m.Content})
	}
	body := map[string]any{
		"model":             c.opts.Model,
		"input":             input,
		"max_output_tokens": c.opts.MaxTokens,
	}
	if system != "" {
		body["instructions"] = system
	}
	data, err := c.post(ctx, "/responses", body)
	if err != nil {
		return Reply{}, err
	}
	var resp responsesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return Reply{}, fmt.Errorf("unreadable responses answer: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return Reply{}, &HTTPError{Code: 400, Body: resp.Error.Message}
	}
	var sb strings.Builder
	for _, item := range resp.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" {
				sb.WriteString(part.Text)
			}
		}
	}
	reply := Reply{Content: strings.TrimSpace(sb.String())}
	if resp.Usage != nil {
		reply.Usage = Usage{Input: resp.Usage.Input, Output: resp.Usage.Output}
	}
	if reply.Content == "" {
		return Reply{}, errors.New("the provider returned no text")
	}
	return reply, nil
}

// --- anthropic -----------------------------------------------------------------------------

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage *struct {
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *HTTPClient) callAnthropic(ctx context.Context, messages []Message) (Reply, error) {
	system, rest := splitSystem(messages)
	conversation := make([]map[string]any, 0, len(rest))
	for _, m := range rest {
		role := "user"
		if m.Role == "assistant" {
			role = "assistant"
		}
		conversation = append(conversation, map[string]any{"role": role, "content": m.Content})
	}
	body := map[string]any{
		"model":      c.opts.Model,
		"messages":   conversation,
		"max_tokens": c.opts.MaxTokens,
	}
	if c.opts.Temperature != nil {
		body["temperature"] = *c.opts.Temperature
	}
	if system != "" {
		body["system"] = system
	}
	data, err := c.post(ctx, "/v1/messages", body)
	if err != nil {
		return Reply{}, err
	}
	var resp anthropicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return Reply{}, fmt.Errorf("unreadable Anthropic response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return Reply{}, &HTTPError{Code: 400, Body: resp.Error.Message}
	}
	var sb strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	reply := Reply{Content: strings.TrimSpace(sb.String())}
	if resp.Usage != nil {
		reply.Usage = Usage{Input: resp.Usage.Input, Output: resp.Usage.Output}
	}
	if reply.Content == "" {
		return Reply{}, errors.New("anthropic returned a response with no text")
	}
	return reply, nil
}

// --- gemini --------------------------------------------------------------------------------

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text    string `json:"text"`
				Thought bool   `json:"thought"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		Prompt     int `json:"promptTokenCount"`
		Candidates int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *HTTPClient) callGemini(ctx context.Context, messages []Message) (Reply, error) {
	system, rest := splitSystem(messages)
	contents := make([]map[string]any, 0, len(rest))
	for _, m := range rest {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{"role": role, "parts": []map[string]any{{"text": m.Content}}})
	}
	body := map[string]any{
		"contents": contents,
		"generationConfig": map[string]any{
			"maxOutputTokens": c.opts.MaxTokens,
			"temperature":     c.temperature(0.7),
		},
	}
	if system != "" {
		body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": system}}}
	}
	data, err := c.post(ctx, "/v1beta/models/"+c.opts.Model+":generateContent", body)
	if err != nil {
		return Reply{}, err
	}
	var resp geminiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return Reply{}, fmt.Errorf("unreadable Gemini response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return Reply{}, &HTTPError{Code: 400, Body: resp.Error.Message}
	}
	if len(resp.Candidates) == 0 {
		return Reply{}, errors.New("gemini returned no candidates (safety block?)")
	}
	var sb strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		if !part.Thought {
			sb.WriteString(part.Text)
		}
	}
	reply := Reply{Content: strings.TrimSpace(sb.String())}
	if resp.UsageMetadata != nil {
		reply.Usage = Usage{Input: resp.UsageMetadata.Prompt, Output: resp.UsageMetadata.Candidates}
	}
	if reply.Content == "" {
		return Reply{}, fmt.Errorf("gemini returned an empty answer (finishReason=%q)", resp.Candidates[0].FinishReason)
	}
	return reply, nil
}
