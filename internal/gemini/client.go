package gemini

import (
	"context"
	"encoding/base64"
	"eva-mind/internal/config"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type Client struct {
	genaiClient *genai.Client
	session     *genai.ChatSession
	cfg         *config.Config
	mu          sync.Mutex
	// Canal para enviar respostas decodificadas para o handler (map simples para compatibilidade)
	RespChan chan map[string]interface{}
	stopChan chan struct{}
}

func NewClient(ctx context.Context, cfg *config.Config) (*Client, error) {
	log.Printf("🔌 Inicializando Gemini SDK Client...")
	client, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GoogleAPIKey))
	if err != nil {
		log.Printf("❌ Erro ao criar cliente Gemini: %v", err)
		return nil, err
	}

	return &Client{
		genaiClient: client,
		cfg:         cfg,
		RespChan:    make(chan map[string]interface{}, 100),
		stopChan:    make(chan struct{}),
	}, nil
}

func (c *Client) SendSetup(instructions string, tools []interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	log.Printf("⚙️ Configurando Modelo Gemini: %s", c.cfg.ModelID)
	model := c.genaiClient.GenerativeModel(c.cfg.ModelID)

	model.SystemInstruction = genai.NewUserContent(genai.Text(instructions))

	// Configurar Audio como resposta se disponível/necessário
	// model.ResponseModalities = []string{"AUDIO"}

	log.Printf("🚀 Iniciando Chat Session...")
	c.session = model.StartChat()

	return nil
}

func (c *Client) SendAudio(audioData []byte) error {
	c.mu.Lock()
	session := c.session
	c.mu.Unlock()

	if session == nil {
		return fmt.Errorf("sessão não iniciada")
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		iter := session.SendMessageStream(ctx, genai.Blob{
			MIMEType: "audio/pcm;rate=16000",
			Data:     audioData,
		})

		for {
			resp, err := iter.Next()
			if err != nil {
				break
			}
			c.processResponse(resp)
		}
	}()

	return nil
}

func (c *Client) processResponse(resp *genai.GenerateContentResponse) {
	if resp == nil || len(resp.Candidates) == 0 {
		return
	}

	candidate := resp.Candidates[0]
	if candidate.Content == nil {
		return
	}

	partsSlice := []interface{}{}

	for _, part := range candidate.Content.Parts {
		if txt, ok := part.(genai.Text); ok {
			partsSlice = append(partsSlice, map[string]interface{}{
				"text": string(txt),
			})
		} else if blob, ok := part.(genai.Blob); ok {
			// Encode bytes to base64 string for handler compatibility
			encoded := base64.StdEncoding.EncodeToString(blob.Data)

			partsSlice = append(partsSlice, map[string]interface{}{
				"inlineData": map[string]interface{}{
					"mimeType": blob.MIMEType,
					"data":     encoded,
				},
			})
		}
	}

	responseMap := map[string]interface{}{
		"serverContent": map[string]interface{}{
			"modelTurn": map[string]interface{}{
				"parts": partsSlice,
			},
		},
	}

	select {
	case c.RespChan <- responseMap:
	default:
		log.Printf("⚠️ Canal de resposta cheio, dropando msg")
	}
}

func (c *Client) ReadResponse() (map[string]interface{}, error) {
	select {
	case resp := <-c.RespChan:
		return resp, nil
	case <-time.After(100 * time.Millisecond):
		return nil, nil
	}
}

func (c *Client) Close() error {
	close(c.stopChan)
	if c.genaiClient != nil {
		return c.genaiClient.Close()
	}
	return nil
}
