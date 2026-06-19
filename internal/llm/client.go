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
	"fmt"
	"net/http"
	"strings"
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
	Error    string `json:"error"` // モデル未取得などは 200 でこのフィールドに入る
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
	if out.Error != "" {
		return "", fmt.Errorf("ollama: %s", out.Error) // モデル未取得など
	}
	return out.Response, nil
}

// Available は Ollama が起動しているかを確認する。
func (o *Ollama) Available(ctx context.Context) bool {
	_, err := o.tags(ctx)
	return err == nil
}

// HasModel は指定モデルが取得済みかを確認する（タグ違いも緩く許容）。
func (o *Ollama) HasModel(ctx context.Context) bool {
	names, err := o.tags(ctx)
	if err != nil {
		return false
	}
	for _, n := range names {
		if n == o.Model || n == o.Model+":latest" || strings.HasPrefix(n, o.Model+":") {
			return true
		}
	}
	return false
}

// tags は取得済みモデル名の一覧を返す。
func (o *Ollama) tags(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.Endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama tags: status %d", resp.StatusCode)
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
