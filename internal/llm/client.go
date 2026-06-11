// Package llm は LLM ランタイム（Ollama）への薄い HTTP クライアント。
// 設計: docs/06-tech-stack.md（Go集約 / Ollama HTTP API）
//
// 役割は「プロンプトを渡して生成テキストを受け取る」だけ。RAG・埋め込み等は持たない。
// Ollama が無い環境でも動くよう、Mock クライアントを同梱する。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Client は LLM 呼び出しの抽象。GM/NPC モジュールはこれに依存する。
type Client interface {
	Generate(ctx context.Context, system, user string) (string, error)
	Name() string
}

// --- Ollama 実装 ---

type Ollama struct {
	Endpoint string
	Model    string
	http     *http.Client
}

func NewOllama(endpoint, model string) *Ollama {
	return &Ollama{
		Endpoint: endpoint,
		Model:    model,
		http:     &http.Client{Timeout: 120 * time.Second},
	}
}

func (o *Ollama) Name() string { return "ollama:" + o.Model }

type ollamaReq struct {
	Model   string         `json:"model"`
	System  string         `json:"system"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Options map[string]any `json:"options,omitempty"`
}

type ollamaResp struct {
	Response string `json:"response"`
}

func (o *Ollama) Generate(ctx context.Context, system, user string) (string, error) {
	body, _ := json.Marshal(ollamaReq{
		Model:  o.Model,
		System: system,
		Prompt: user,
		Stream: false,
		Options: map[string]any{
			"temperature": 0.8,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.Endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var out ollamaResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Response, nil
}

// Available は Ollama が起動しているかを確認する。
func (o *Ollama) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.Endpoint+"/api/tags", nil)
	if err != nil {
		return false
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
