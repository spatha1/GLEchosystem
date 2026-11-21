// Package ai lets an AI model propose GL configuration from a business requirements document
// (BRD), one step at a time: business events, then measures, then formulas, then the debit
// and credit lines with test cases. It never decides anything: every proposal is checked by
// the engine, shown to a person, and saved to a draft only when that person approves it; the
// version is still validated and approved by someone who did not edit it.
//
// The provider key is read from a settings file on the server each time it is needed. It is
// never logged, stored in the database or sent to the browser. Requests carry the BRD text and
// the configuration (codes, names, rules), never policy or customer data: mapping and data
// collection are separate and never pass through the AI.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Message is one turn of a conversation with the model.
type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

// Usage is the size of one request.
type Usage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
}

func (u *Usage) add(o Usage) {
	u.PromptTokens += o.PromptTokens
	u.CompletionTokens += o.CompletionTokens
}

// Status says whether AI drafting is available, and why not.
type Status struct {
	Available bool   `json:"available"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Drafter asks a model for one JSON object.
type Drafter interface {
	Status() Status
	JSON(ctx context.Context, msgs []Message) (string, Usage, error)
}

// ErrProvider is a failure the provider reported, in words safe to show.
type ErrProvider string

func (e ErrProvider) Error() string { return string(e) }

// Settings is how the provider is reached. It is the same file format as the QA Workbench's
// (AI_PROVIDER, OPENAI_API_KEY, OPENAI_MODEL, OPENAI_BASE_URL, AI_ENABLED), so one file can serve both.
type Settings struct {
	Provider string
	Model    string
	Enabled  bool
	Reason   string // why AI is not available; empty when it is
	key      string
	base     string
}

// DefaultModel is used when the settings file names none.
const DefaultModel = "gpt-4.1"

// Load reads the settings file: lines of NAME=value, # for comments.
func Load(path string) Settings {
	if strings.TrimSpace(path) == "" {
		return Settings{Reason: "AI is not set up on this server: no AI settings file is configured (GL_AI_SETTINGS)."}
	}
	f, err := os.Open(path)
	if err != nil {
		return Settings{Reason: "AI is not set up on this server: the AI settings file was not found."}
	}
	defer f.Close()
	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
			kv[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return parse(kv)
}

func parse(kv map[string]string) Settings {
	s := Settings{Provider: strings.ToLower(kv["AI_PROVIDER"]), Model: kv["OPENAI_MODEL"], key: kv["OPENAI_API_KEY"], Enabled: true,
		base: strings.TrimRight(kv["OPENAI_BASE_URL"], "/")}
	// Only HTTPS, or this machine, may receive the key.
	if s.base == "" || !(strings.HasPrefix(s.base, "https://") || strings.HasPrefix(s.base, "http://127.0.0.1:") || strings.HasPrefix(s.base, "http://localhost:")) {
		s.base = "https://api.openai.com/v1"
	}
	if v := strings.ToLower(kv["AI_ENABLED"]); v == "0" || v == "false" || v == "no" || v == "off" {
		s.Enabled = false
	}
	if s.Provider == "" && s.key != "" {
		s.Provider = "openai"
	}
	if s.Model == "" {
		s.Model = DefaultModel
	}
	switch {
	case !s.Enabled:
		s.Reason = "AI is switched off in the AI settings file (AI_ENABLED)."
	case s.Provider != "openai":
		s.Reason = fmt.Sprintf("The AI settings file names the provider %q; the GL studio drafts with OpenAI only.", s.Provider)
	case s.key == "":
		s.Reason = "The AI settings file has no OpenAI key (OPENAI_API_KEY)."
	}
	return s
}

// Status reports availability without the key.
func (s Settings) Status() Status {
	if s.Reason != "" {
		return Status{Reason: s.Reason, Provider: s.Provider}
	}
	return Status{Available: true, Provider: s.Provider, Model: s.Model}
}

// Timeout is how long one request may take.
const Timeout = 180 * time.Second

var httpClient = &http.Client{Timeout: Timeout}

// JSON asks the model for one JSON object (OpenAI chat completions, JSON mode) and returns its text.
func (s Settings) JSON(ctx context.Context, msgs []Message) (string, Usage, error) {
	if s.Reason != "" {
		return "", Usage{}, ErrProvider(s.Reason)
	}
	payload := map[string]any{
		"model":           s.Model,
		"response_format": map[string]string{"type": "json_object"},
		"messages":        msgs,
		"temperature":     0.1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", Usage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", Usage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.key)
	res, err := httpClient.Do(req)
	if err != nil {
		var ne interface{ Timeout() bool }
		if errors.As(err, &ne) && ne.Timeout() {
			return "", Usage{}, ErrProvider("The AI provider did not answer in time.")
		}
		return "", Usage{}, ErrProvider("The AI provider could not be reached.")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return "", Usage{}, ErrProvider("The AI provider's answer could not be read.")
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	u := Usage{PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens}
	if res.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("The AI provider answered with an error (HTTP %d).", res.StatusCode)
		if out.Error != nil && out.Error.Message != "" {
			msg += " It says: " + strings.ReplaceAll(out.Error.Message, s.key, "[key]")
		}
		return "", u, ErrProvider(msg)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", u, ErrProvider("The AI provider returned an empty answer.")
	}
	if out.Choices[0].FinishReason == "length" {
		return "", u, ErrProvider("The AI provider's answer was cut off (too long). Try a shorter BRD or one step's part of it.")
	}
	return out.Choices[0].Message.Content, u, nil
}
