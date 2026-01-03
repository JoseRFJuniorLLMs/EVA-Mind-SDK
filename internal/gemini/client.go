package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"eva-mind/internal/config"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// AudioCallback é chamado quando áudio PCM é recebido do Gemini
type AudioCallback func(audioBytes []byte)

// ToolCallCallback é chamado quando uma ferramenta precisa ser executada
type ToolCallCallback func(name string, args map[string]interface{}) map[string]interface{}

// Client gerencia a conexão WebSocket com Gemini Live API
type Client struct {
	conn       *websocket.Conn
	mu         sync.Mutex
	cfg        *config.Config
	onAudio    AudioCallback
	onToolCall ToolCallCallback
}

// NewClient cria um novo cliente Gemini usando WebSocket direto
func NewClient(ctx context.Context, cfg *config.Config) (*Client, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	url := fmt.Sprintf("wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent?key=%s", cfg.GoogleAPIKey)

	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar no websocket: %w", err)
	}

	return &Client{conn: conn, cfg: cfg}, nil
}

// SetCallbacks configura os retornos de áudio e ferramentas (exigido pelo main.go:295)
func (c *Client) SetCallbacks(onAudio AudioCallback, onToolCall ToolCallCallback) {
	c.onAudio = onAudio
	c.onToolCall = onToolCall
}

// SendSetup envia configuração inicial (exigido pelo signaling/websocket.go)
func (c *Client) SendSetup(instructions string, tools []interface{}) error {
	setupMsg := map[string]interface{}{
		"setup": map[string]interface{}{
			"model": fmt.Sprintf("models/%s", c.cfg.ModelID),
			"generation_config": map[string]interface{}{
				"response_modalities": []string{"AUDIO"},
				"speech_config": map[string]interface{}{
					"voice_config": map[string]interface{}{
						"prebuilt_voice_config": map[string]string{
							"voice_name": "Aoede",
						},
					},
				},
			},
			"system_instruction": map[string]interface{}{
				"parts": []map[string]string{
					{"text": instructions},
				},
			},
			"tools": tools,
		},
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(setupMsg)
}

// StartSession é um alias para SendSetup
func (c *Client) StartSession(instructions string, tools []interface{}) error {
	return c.SendSetup(instructions, tools)
}

// SendAudio envia dados de áudio PCM para o Gemini
func (c *Client) SendAudio(audioData []byte) error {
	encoded := base64.StdEncoding.EncodeToString(audioData)

	msg := map[string]interface{}{
		"realtime_input": map[string]interface{}{
			"media_chunks": []map[string]string{
				{
					"mime_type": "audio/pcm;rate=16000",
					"data":      encoded,
				},
			},
		},
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(msg)
}

// ReadResponse lê a próxima resposta bruta do WebSocket
func (c *Client) ReadResponse() (map[string]interface{}, error) {
	var response map[string]interface{}
	err := c.conn.ReadJSON(&response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// HandleResponses processa o loop de mensagens (exigido pelo main.go:318)
func (c *Client) HandleResponses(ctx context.Context) error {
	log.Printf("👂 HandleResponses: loop iniciado")

	for {
		select {
		case <-ctx.Done():
			log.Printf("🛑 HandleResponses: contexto cancelado")
			return ctx.Err()
		default:
			resp, err := c.ReadResponse()
			if err != nil {
				log.Printf("❌ Erro ao ler resposta: %v", err)
				return err
			}

			// ✅ DEBUG: Mostrar estrutura da resposta
			if respBytes, _ := json.Marshal(resp); len(respBytes) > 0 {
				preview := string(respBytes)
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				log.Printf("📦 Resposta Gemini: %s", preview)
			}

			// ✅ Processar áudio
			if serverContent, ok := resp["serverContent"].(map[string]interface{}); ok {
				if modelTurn, ok := serverContent["modelTurn"].(map[string]interface{}); ok {
					if parts, ok := modelTurn["parts"].([]interface{}); ok {
						log.Printf("📋 Processando %d parts", len(parts))

						for _, p := range parts {
							part, ok := p.(map[string]interface{})
							if !ok {
								continue
							}

							// ✅ Procurar por inlineData (áudio)
							if inlineData, ok := part["inlineData"].(map[string]interface{}); ok {
								log.Printf("🎵 inlineData encontrado")

								if audioB64, ok := inlineData["data"].(string); ok {
									audioBytes, err := base64.StdEncoding.DecodeString(audioB64)
									if err != nil {
										log.Printf("❌ Erro ao decodificar base64: %v", err)
										continue
									}

									log.Printf("✅ Áudio decodificado: %d bytes", len(audioBytes))

									// ✅ CHAMAR CALLBACK
									if c.onAudio != nil {
										log.Printf("📞 Chamando callback onAudio...")
										c.onAudio(audioBytes)
									} else {
										log.Printf("⚠️ CALLBACK onAudio NÃO CONFIGURADO!")
									}
								}
							}
						}
					}
				}
			}

			// ✅ Processar tool calls
			if toolCall, ok := resp["toolCall"].(map[string]interface{}); ok {
				log.Printf("🔧 Tool call detectado")
				c.handleToolCalls(toolCall)
			}
		}
	}
}

func (c *Client) handleToolCalls(toolCall map[string]interface{}) {
	if fcList, ok := toolCall["functionCalls"].([]interface{}); ok {
		for _, f := range fcList {
			fc := f.(map[string]interface{})
			name := fc["name"].(string)
			args := fc["args"].(map[string]interface{})

			if c.onToolCall != nil {
				result := c.onToolCall(name, args)
				c.SendToolResponse(name, result)
			}
		}
	}
}

// SendToolResponse envia o resultado da função de volta ao Gemini
func (c *Client) SendToolResponse(name string, result map[string]interface{}) error {
	msg := map[string]interface{}{
		"tool_response": map[string]interface{}{
			"function_responses": []map[string]interface{}{
				{
					"name":     name,
					"response": result,
				},
			},
		},
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(msg)
}

// Close fecha a conexão
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
