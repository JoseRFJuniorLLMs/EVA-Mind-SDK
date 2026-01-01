package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"eva-mind/internal/config"
	"eva-mind/internal/database"
	"eva-mind/internal/gemini"
	"eva-mind/internal/push"

	"github.com/gorilla/websocket"
)

type PCMWebSocketHandler struct {
	upgrader    websocket.Upgrader
	clients     map[string]*PCMClient
	mu          sync.RWMutex
	cfg         *config.Config
	pushService *push.FirebaseService
	db          *database.DB
}

type PCMClient struct {
	Conn         *websocket.Conn
	CPF          string
	IdosoID      int64
	GeminiClient *gemini.Client
	SendCh       chan []byte
	mu           sync.Mutex
	active       bool
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewPCMWebSocketHandler(cfg *config.Config, pushService *push.FirebaseService, db *database.DB) *PCMWebSocketHandler {
	return &PCMWebSocketHandler{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			ReadBufferSize:  8192,
			WriteBufferSize: 8192,
		},
		clients:     make(map[string]*PCMClient),
		cfg:         cfg,
		pushService: pushService,
		db:          db,
	}
}

func (h *PCMWebSocketHandler) HandlePCMConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("❌ Erro no upgrade WebSocket: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	client := &PCMClient{
		Conn:   conn,
		SendCh: make(chan []byte, 256),
		ctx:    ctx,
		cancel: cancel,
	}

	go h.handleClientSend(client)
	h.handleClientMessages(client)
}

func (h *PCMWebSocketHandler) handleClientMessages(client *PCMClient) {
	defer func() {
		client.cancel()

		if client.CPF != "" {
			h.mu.Lock()
			delete(h.clients, client.CPF)
			h.mu.Unlock()
		}

		if client.GeminiClient != nil {
			client.GeminiClient.Close()
		}

		// Evitar fechar channel duas vezes
		client.mu.Lock()
		if client.SendCh != nil {
			// close(client.SendCh) // Removido para evitar panic em concorrência, o cancel cuida do loop
		}
		client.mu.Unlock()

		client.Conn.Close()
		log.Printf("🔌 Sessão finalizada para o cliente")
	}()

	client.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	client.Conn.SetPongHandler(func(string) error {
		client.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		messageType, message, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("❌ Erro de leitura: %v", err)
			}
			break
		}

		client.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		if messageType == websocket.TextMessage {
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Printf("⚠️ Erro unmarshal JSON: %v", err)
				continue
			}

			msgType, _ := msg["type"].(string)

			switch msgType {
			case "register":
				h.handleRegister(client, msg)
			case "hangup":
				log.Printf("📞 Cliente solicitou desligamento")
				return
			}
		}

		if messageType == websocket.BinaryMessage {
			if !client.active || client.GeminiClient == nil {
				continue
			}

			if err := client.GeminiClient.SendAudio(message); err != nil {
				log.Printf("❌ Erro ao enviar áudio para Gemini: %v", err)
			}
		}
	}
}

func (h *PCMWebSocketHandler) handleClientSend(client *PCMClient) {
	log.Printf("📡 ========================================")
	log.Printf("📡 handleClientSend INICIADO")
	log.Printf("📡 CPF: %s (se disponível)", client.CPF)
	log.Printf("📡 ========================================")

	defer func() {
		log.Printf("🛑 handleClientSend FINALIZADO para CPF: %s", client.CPF)
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	audioPacketCount := 0

	for {
		select {
		case <-client.ctx.Done():
			log.Printf("🔴 Context cancelado em handleClientSend (CPF: %s)", client.CPF)
			return

		case audioData, ok := <-client.SendCh:
			if !ok {
				log.Printf("🔴 SendCh fechado (CPF: %s)", client.CPF)
				return
			}

			audioPacketCount++

			// Log reduzido: apenas a cada 10 pacotes
			if audioPacketCount%10 == 1 || audioPacketCount <= 3 {
				log.Printf("📤 [#%d] Enviando áudio (%d bytes) para CPF: %s", audioPacketCount, len(audioData), client.CPF)
			}

			client.mu.Lock()
			err := client.Conn.WriteMessage(websocket.BinaryMessage, audioData)
			client.mu.Unlock()

			if err != nil {
				log.Printf("❌ ERRO ao enviar áudio #%d para CPF %s: %v", audioPacketCount, client.CPF, err)
				return
			}

			// Log de sucesso apenas para primeiros pacotes ou a cada 10
			if audioPacketCount%10 == 1 || audioPacketCount <= 3 {
				log.Printf("✅ [#%d] Áudio enviado com sucesso para CPF: %s", audioPacketCount, client.CPF)
			}

		case <-ticker.C:
			log.Printf("🏓 Enviando ping para CPF: %s", client.CPF)
			client.mu.Lock()
			err := client.Conn.WriteMessage(websocket.PingMessage, nil)
			client.mu.Unlock()

			if err != nil {
				log.Printf("❌ Erro ao enviar ping (CPF: %s): %v", client.CPF, err)
				return
			}
		}
	}
}

func (h *PCMWebSocketHandler) handleRegister(client *PCMClient, msg map[string]interface{}) {
	cpf, ok := msg["cpf"].(string)
	if !ok || cpf == "" {
		h.sendError(client, "CPF inválido")
		return
	}

	// 1. CHAMA O MÉTODO NOVO (que deve ter a lógica de regexp_replace)
	idoso, err := h.db.GetIdosoByCPF(cpf)
	if err != nil {
		log.Printf("❌ CPF não encontrado no banco: %s", cpf)
		h.sendError(client, "CPF não autorizado ou não cadastrado")
		return
	}

	client.CPF = idoso.CPF
	client.IdosoID = idoso.ID

	h.mu.Lock()
	if existingClient, exists := h.clients[idoso.CPF]; exists {
		log.Printf("♻️ Substituindo conexão existente para o CPF: %s", idoso.CPF)
		existingClient.cancel()
		existingClient.Conn.Close()
	}
	h.clients[idoso.CPF] = client
	h.mu.Unlock()

	log.Printf("✅ Cliente autenticado: %s (ID: %d)", idoso.Nome, idoso.ID)

	// ============================================================================
	// NOVA LÓGICA: Marcar que o idoso atendeu a chamada (WATCHDOG)
	// ============================================================================
	go func() {
		_, err := h.db.GetConnection().Exec(`
			UPDATE agendamentos 
			SET status = 'em_chamada', data_hora_realizada = NOW()
			WHERE idoso_id = $1 
			  AND status IN ('agendado', 'em_andamento', 'aguardando_atendimento')
			  AND data_hora_agendada >= NOW() - INTERVAL '10 minutes'
		`, idoso.ID)

		if err != nil {
			log.Printf("❌ Erro ao atualizar status para 'em_chamada': %v", err)
		} else {
			log.Printf("📞 Idoso %d atendeu a chamada. Status atualizado.", idoso.ID)
		}
	}()
	// ============================================================================

	sessionID := fmt.Sprintf("pcm-session-%d", time.Now().UnixNano())

	// Inicializa o cliente Gemini (Multimodal Live API)
	geminiClient, err := gemini.NewClient(client.ctx, h.cfg)
	if err != nil {
		log.Printf("❌ Erro Gemini NewClient: %v", err)
		h.sendError(client, "Erro na engine de IA")
		return
	}

	client.GeminiClient = geminiClient

	// CONSTRUÇÃO DO PROMPT DINÂMICO
	instructions, err := h.buildInstructionsFromDB(client.IdosoID)
	if err != nil {
		log.Printf("⚠️ Falha ao montar prompt customizado: %v. Usando fallback.", err)
		instructions = `You are EVA, a voice assistant for elderly people in Brazil.
Speak naturally in Brazilian Portuguese. Be warm, patient, and empathetic.
Keep responses very short (1-2 sentences).`
	}

	tools := gemini.GetDefaultTools()

	// Envia o Setup para a API do Gemini
	if err := client.GeminiClient.SendSetup(instructions, tools); err != nil {
		log.Printf("❌ Erro no SendSetup do Gemini: %v", err)
		geminiClient.Close()
		h.sendError(client, "Falha na configuração da sessão de voz")
		return
	}

	// Inicia a escuta das respostas da IA em background
	go h.listenGeminiResponses(client)

	client.active = true

	response := map[string]interface{}{
		"type":      "session_created",
		"sessionId": sessionID,
		"message":   "Sessão criada! EVA está pronta para conversar.",
	}

	h.sendJSON(client, response)
}

func (h *PCMWebSocketHandler) buildInstructionsFromDB(idosoID int64) (string, error) {
	// 1. Buscar detalhes do perfil do idoso
	query := `
		SELECT 
			nome, 
			EXTRACT(YEAR FROM AGE(data_nascimento)) as idade,
			nivel_cognitivo, 
			limitacoes_auditivas, 
			usa_aparelho_auditivo, 
			tom_voz
		FROM idosos 
		WHERE id = $1
	`

	var nome, nivelCognitivo, tomVoz string
	var idade int
	var limitacoesAuditivas, usaAparelhoAuditivo bool

	err := h.db.GetConnection().QueryRow(query, idosoID).Scan(
		&nome,
		&idade,
		&nivelCognitivo,
		&limitacoesAuditivas,
		&usaAparelhoAuditivo,
		&tomVoz,
	)

	if err != nil {
		return "", fmt.Errorf("erro query idoso: %v", err)
	}

	// 2. Buscar o template de prompt base
	templateQuery := `
		SELECT template, variaveis_esperadas
		FROM prompt_templates
		WHERE nome = 'eva_base_v2' AND ativo = true
		LIMIT 1
	`

	var template string
	var variaveis string

	err = h.db.GetConnection().QueryRow(templateQuery).Scan(&template, &variaveis)
	if err != nil {
		return "", fmt.Errorf("erro query template: %v", err)
	}

	// 3. Processamento manual das variáveis do template (Lógica de Substituição)
	instructions := strings.ReplaceAll(template, "{{nome_idoso}}", nome)
	instructions = strings.ReplaceAll(instructions, "{{idade}}", fmt.Sprintf("%d", idade))
	instructions = strings.ReplaceAll(instructions, "{{nivel_cognitivo}}", nivelCognitivo)
	instructions = strings.ReplaceAll(instructions, "{{tom_voz}}", tomVoz)

	// Lógica para Limitações Auditivas
	if limitacoesAuditivas {
		instructions = strings.ReplaceAll(instructions, "{{#limitacoes_auditivas}}", "")
		instructions = strings.ReplaceAll(instructions, "{{/limitacoes_auditivas}}", "")
	} else {
		start := strings.Index(instructions, "{{#limitacoes_auditivas}}")
		end := strings.Index(instructions, "{{/limitacoes_auditivas}}")
		if start != -1 && end != -1 {
			instructions = instructions[:start] + instructions[end+len("{{/limitacoes_auditivas}}"):]
		}
	}

	// Lógica para Aparelho Auditivo
	if usaAparelhoAuditivo {
		instructions = strings.ReplaceAll(instructions, "{{#usa_aparelho_auditivo}}", "")
		instructions = strings.ReplaceAll(instructions, "{{/usa_aparelho_auditivo}}", "")
	} else {
		start := strings.Index(instructions, "{{#usa_aparelho_auditivo}}")
		end := strings.Index(instructions, "{{/usa_aparelho_auditivo}}")
		if start != -1 && end != -1 {
			instructions = instructions[:start] + instructions[end+len("{{/usa_aparelho_auditivo}}"):]
		}
	}

	// Limpeza de placeholders residuais e constantes de negócio
	instructions = strings.ReplaceAll(instructions, "{{#primeira_interacao}}", "")
	instructions = strings.ReplaceAll(instructions, "{{/primeira_interacao}}", "")
	instructions = strings.ReplaceAll(instructions, "{{^primeira_interacao}}", "")
	instructions = strings.ReplaceAll(instructions, "{{taxa_adesao}}", "85")

	return instructions, nil
}

func (h *PCMWebSocketHandler) listenGeminiResponses(client *PCMClient) {
	log.Printf("🎧 ========================================")
	log.Printf("🎧 INICIANDO listenGeminiResponses")
	log.Printf("🎧 CPF: %s", client.CPF)
	log.Printf("🎧 IdosoID: %d", client.IdosoID)
	log.Printf("🎧 GeminiClient existe: %v", client.GeminiClient != nil)
	log.Printf("🎧 Client active: %v", client.active)
	log.Printf("🎧 Context existe: %v", client.ctx != nil)
	log.Printf("🎧 ========================================")

	defer func() {
		if r := recover(); r != nil {
			log.Printf("💥 ========================================")
			log.Printf("💥 PANIC DETECTADO em listenGeminiResponses!")
			log.Printf("💥 CPF: %s", client.CPF)
			log.Printf("💥 Panic: %v", r)
			log.Printf("💥 ========================================")
		}
		log.Printf("🛑 listenGeminiResponses FINALIZADO para CPF: %s", client.CPF)
	}()

	iterationCount := 0
	lastLogTime := time.Now()

	for {
		select {
		case <-client.ctx.Done():
			log.Printf("🔴 Context cancelado para CPF %s, saindo do loop", client.CPF)
			return
		default:
			if !client.active {
				log.Printf("🔴 Client não está ativo para CPF %s, saindo do loop", client.CPF)
				return
			}

			iterationCount++

			// Log a cada 10 iterações OU a cada 5 segundos
			now := time.Now()
			shouldLog := (iterationCount%10 == 1) || (now.Sub(lastLogTime) > 5*time.Second)

			if shouldLog {
				log.Printf("🔄 ========================================")
				log.Printf("🔄 listenGeminiResponses ITERAÇÃO #%d", iterationCount)
				log.Printf("🔄 CPF: %s", client.CPF)
				log.Printf("🔄 Aguardando resposta do Gemini...")
				log.Printf("🔄 ========================================")
				lastLogTime = now
			}

			log.Printf("📞 [Iter %d] Chamando ReadResponse()...", iterationCount)
			response, err := client.GeminiClient.ReadResponse()

			if err != nil {
				log.Printf("⚠️ ========================================")
				log.Printf("⚠️ ERRO em ReadResponse (Iteração #%d)", iterationCount)
				log.Printf("⚠️ CPF: %s", client.CPF)
				log.Printf("⚠️ Erro: %v", err)
				log.Printf("⚠️ Tipo do erro: %T", err)

				if client.ctx.Err() != nil {
					log.Printf("🔴 Context error detectado: %v", client.ctx.Err())
					log.Printf("⚠️ ========================================")
					return
				}

				log.Printf("⚠️ Aplicando backoff de 100ms...")
				log.Printf("⚠️ ========================================")

				// Pequeno backoff para não fritar a CPU em erro de leitura
				time.Sleep(100 * time.Millisecond)
				continue
			}

			log.Printf("✅ ========================================")
			log.Printf("✅ RESPOSTA RECEBIDA DO GEMINI (Iteração #%d)", iterationCount)
			log.Printf("✅ CPF: %s", client.CPF)
			log.Printf("✅ Response não é nil: %v", response != nil)
			if response != nil {
				log.Printf("✅ Response keys: %v", getMapKeys(response))
			}
			log.Printf("✅ Chamando handleGeminiResponse()...")
			log.Printf("✅ ========================================")

			h.handleGeminiResponse(client, response)

			log.Printf("✅ handleGeminiResponse() CONCLUÍDO (Iteração #%d)", iterationCount)
		}
	}
}

// Helper function para logar as chaves de um map
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func (h *PCMWebSocketHandler) handleGeminiResponse(client *PCMClient, response map[string]interface{}) {
	log.Printf("🔍 ========================================")
	log.Printf("🔍 handleGeminiResponse INICIADO")
	log.Printf("🔍 CPF: %s", client.CPF)
	log.Printf("🔍 Response keys: %v", getMapKeys(response))
	log.Printf("🔍 ========================================")

	// Ignorar mensagens puramente de controle de setup
	if setupComplete, ok := response["setupComplete"].(bool); ok && setupComplete {
		log.Printf("✅ Gemini Setup Concluído via WebSocket (CPF: %s)", client.CPF)
		return
	}

	serverContent, ok := response["serverContent"].(map[string]interface{})
	if !ok {
		log.Printf("⚠️ Response NÃO contém 'serverContent' (CPF: %s)", client.CPF)
		return
	}
	log.Printf("✅ serverContent encontrado (CPF: %s)", client.CPF)

	modelTurn, ok := serverContent["modelTurn"].(map[string]interface{})
	if !ok {
		log.Printf("⚠️ serverContent NÃO contém 'modelTurn' (CPF: %s)", client.CPF)
		return
	}
	log.Printf("✅ modelTurn encontrado (CPF: %s)", client.CPF)

	parts, ok := modelTurn["parts"].([]interface{})
	if !ok {
		log.Printf("⚠️ modelTurn NÃO contém 'parts' (CPF: %s)", client.CPF)
		return
	}
	log.Printf("✅ parts encontrado - Total: %d parts (CPF: %s)", len(parts), client.CPF)

	for i, part := range parts {
		log.Printf("🔍 Processando part #%d/%d (CPF: %s)", i+1, len(parts), client.CPF)

		partMap, ok := part.(map[string]interface{})
		if !ok {
			log.Printf("⚠️ Part #%d não é um map (CPF: %s)", i+1, client.CPF)
			continue
		}

		log.Printf("🔍 Part #%d keys: %v (CPF: %s)", i+1, getMapKeys(partMap), client.CPF)

		// 1. Processamento de Áudio PCM vindo da IA
		if inlineData, ok := partMap["inlineData"].(map[string]interface{}); ok {
			log.Printf("🎵 ========================================")
			log.Printf("🎵 INLINE DATA DETECTADO (Part #%d)", i+1)
			log.Printf("🎵 CPF: %s", client.CPF)

			mimeType, _ := inlineData["mimeType"].(string)
			data, hasData := inlineData["data"].(string)

			log.Printf("🎵 mimeType: %s", mimeType)
			log.Printf("🎵 hasData: %v", hasData)

			if hasData {
				log.Printf("🎵 data length (base64): %d chars", len(data))
			}

			if hasData && strings.HasPrefix(mimeType, "audio/pcm") {
				log.Printf("✅ ========================================")
				log.Printf("✅ ÁUDIO PCM CONFIRMADO!")
				log.Printf("✅ CPF: %s", client.CPF)
				log.Printf("✅ Iniciando decode base64...")
				log.Printf("✅ ========================================")

				audioData, err := base64.StdEncoding.DecodeString(data)
				if err != nil {
					log.Printf("❌ ========================================")
					log.Printf("❌ ERRO ao decodificar base64!")
					log.Printf("❌ CPF: %s", client.CPF)
					log.Printf("❌ Erro: %v", err)
					log.Printf("❌ ========================================")
					continue
				}

				log.Printf("📦 ========================================")
				log.Printf("📦 ÁUDIO DECODIFICADO COM SUCESSO!")
				log.Printf("📦 CPF: %s", client.CPF)
				log.Printf("📦 Tamanho: %d bytes", len(audioData))
				log.Printf("📦 Primeiros 10 bytes: %v", audioData[:min(10, len(audioData))])
				log.Printf("📦 ========================================")

				log.Printf("🔊 Tentando enviar para SendCh...")
				log.Printf("🔊 SendCh buffer capacity: %d", cap(client.SendCh))
				log.Printf("🔊 SendCh buffer length: %d", len(client.SendCh))

				// Envia o chunk de áudio para o canal de saída do cliente
				select {
				case client.SendCh <- audioData:
					log.Printf("✅ ========================================")
					log.Printf("✅ ÁUDIO ENVIADO PARA SendCh COM SUCESSO!")
					log.Printf("✅ CPF: %s", client.CPF)
					log.Printf("✅ Bytes enviados: %d", len(audioData))
					log.Printf("✅ ========================================")
				case <-time.After(1 * time.Second):
					log.Printf("⚠️ ========================================")
					log.Printf("⚠️ TIMEOUT ao enviar para SendCh!")
					log.Printf("⚠️ CPF: %s", client.CPF)
					log.Printf("⚠️ O canal pode estar bloqueado ou cheio")
					log.Printf("⚠️ ========================================")
				}
			} else {
				log.Printf("⚠️ inlineData NÃO é áudio PCM ou está vazio (CPF: %s)", client.CPF)
				log.Printf("⚠️ mimeType: %s, hasData: %v", mimeType, hasData)
			}
			log.Printf("🎵 ========================================")
		}

		// 2. Processamento de Chamada de Ferramentas (Function Calling)
		if fnCall, ok := partMap["functionCall"].(map[string]interface{}); ok {
			fnName, _ := fnCall["name"].(string)
			log.Printf("🛠️ Function Call detectado: %s (CPF: %s)", fnName, client.CPF)
			h.executeTool(client, fnCall)
		}
	}

	log.Printf("🔍 handleGeminiResponse CONCLUÍDO (CPF: %s)", client.CPF)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (h *PCMWebSocketHandler) executeTool(client *PCMClient, fnCall map[string]interface{}) {
	name, _ := fnCall["name"].(string)
	args, _ := fnCall["args"].(map[string]interface{})

	log.Printf("🛠️ IA solicitou ferramenta: %s", name)

	switch name {
	case "alert_family":
		reason, _ := args["reason"].(string)
		log.Printf("🚨 Alerta de emergência! Razão: %s", reason)

		if err := gemini.AlertFamily(h.db.GetConnection(), h.pushService, client.IdosoID, reason); err != nil {
			log.Printf("❌ Erro ao disparar alerta para família: %v", err)
		}

	case "confirm_medication":
		medication, _ := args["medication_name"].(string)
		log.Printf("💊 Confirmação de remédio: %s", medication)

		if err := gemini.ConfirmMedication(h.db.GetConnection(), h.pushService, client.IdosoID, medication); err != nil {
			log.Printf("❌ Erro ao registrar medicamento no DB: %v", err)
		}
	}
}

func (h *PCMWebSocketHandler) sendJSON(client *PCMClient, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}

	client.mu.Lock()
	defer client.mu.Unlock()

	if err := client.Conn.WriteMessage(websocket.TextMessage, jsonData); err != nil {
		log.Printf("❌ Erro ao enviar JSON: %v", err)
	}
}

func (h *PCMWebSocketHandler) sendError(client *PCMClient, message string) {
	h.sendJSON(client, map[string]interface{}{
		"type":    "error",
		"message": message,
	})
}

func (h *PCMWebSocketHandler) GetActiveClientsCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
