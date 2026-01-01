package signaling

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"eva-mind/internal/config"
	"eva-mind/internal/gemini"
	"eva-mind/internal/push"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type WebSocketSession struct {
	ID           string
	CPF          string
	IdosoID      int64
	WSConn       *websocket.Conn
	GeminiClient *gemini.Client
	ctx          context.Context
	cancel       context.CancelFunc
	lastActivity time.Time
	mu           sync.RWMutex
}

type SignalingServer struct {
	cfg         *config.Config
	db          *sql.DB
	pushService *push.FirebaseService
	sessions    sync.Map
	clients     sync.Map
}

func NewSignalingServer(cfg *config.Config, db *sql.DB, pushService *push.FirebaseService) *SignalingServer {
	server := &SignalingServer{
		cfg:         cfg,
		db:          db,
		pushService: pushService,
	}
	go server.cleanupDeadSessions()
	return server
}

func (s *SignalingServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	var currentSession *WebSocketSession

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		switch messageType {
		case websocket.TextMessage:
			currentSession = s.handleControlMessage(conn, message, currentSession)

		case websocket.BinaryMessage:
			if currentSession != nil {
				s.handleAudioMessage(currentSession, message)
			}
		}
	}

	if currentSession != nil {
		s.cleanupSession(currentSession.ID)
	}
}

func (s *SignalingServer) handleControlMessage(conn *websocket.Conn, message []byte, currentSession *WebSocketSession) *WebSocketSession {
	var msg ControlMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		return currentSession
	}

	switch msg.Type {
	case "register":
		_, err := s.getIdosoByCPF(msg.CPF)
		if err != nil {
			s.sendError(conn, "CPF não encontrado")
			return currentSession
		}

		s.clients.Store(msg.CPF, conn)
		log.Printf("👤 Cliente registrado: %s", msg.CPF)

		s.sendMessage(conn, ControlMessage{
			Type:    "registered",
			Success: true,
		})

		return currentSession

	case "start_call":
		if msg.SessionID == "" {
			msg.SessionID = generateSessionID()
		}

		idoso, err := s.getIdosoByCPF(msg.CPF)
		if err != nil {
			s.sendError(conn, "CPF não encontrado")
			return currentSession
		}

		session, err := s.createSession(msg.SessionID, msg.CPF, idoso.ID, conn)
		if err != nil {
			s.sendError(conn, "Erro ao criar sessão")
			return currentSession
		}

		go s.audioClientToGemini(session)
		go s.audioGeminiToClient(session)

		s.sendMessage(conn, ControlMessage{
			Type:      "session_created",
			SessionID: msg.SessionID,
			Success:   true,
		})

		log.Printf("📞 Chamada iniciada: %s", msg.CPF)
		return session

	case "hangup":
		if currentSession != nil {
			s.cleanupSession(currentSession.ID)
		}
		return nil

	case "ping":
		s.sendMessage(conn, ControlMessage{Type: "pong"})
		return currentSession

	default:
		return currentSession
	}
}

func (s *SignalingServer) handleAudioMessage(session *WebSocketSession, pcmData []byte) {
	session.mu.Lock()
	session.lastActivity = time.Now()
	session.mu.Unlock()

	if err := session.GeminiClient.SendAudio(pcmData); err != nil {
		log.Printf("❌ Erro ao enviar áudio para Gemini")
	}
}

func (s *SignalingServer) audioClientToGemini(session *WebSocketSession) {
	<-session.ctx.Done()
}

func (s *SignalingServer) audioGeminiToClient(session *WebSocketSession) {
	for {
		select {
		case <-session.ctx.Done():
			return
		default:
			response, err := session.GeminiClient.ReadResponse()
			if err != nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			s.handleGeminiResponse(session, response)
		}
	}
}

func (s *SignalingServer) handleGeminiResponse(session *WebSocketSession, response map[string]interface{}) {
	if setupComplete, ok := response["setupComplete"].(bool); ok && setupComplete {
		return
	}

	// Processar serverContent
	serverContent, ok := response["serverContent"].(map[string]interface{})
	if !ok {
		return
	}

	// ========== TRANSCRIÇÃO NATIVA (NOVO) ==========
	// Capturar transcrição do USUÁRIO (input audio)
	if inputTrans, ok := serverContent["inputAudioTranscription"].(map[string]interface{}); ok {
		if userText, ok := inputTrans["text"].(string); ok && userText != "" {
			log.Printf("🗣️ [NATIVE] IDOSO: %s", userText)
			go s.saveTranscription(session.IdosoID, "user", userText)
		}
	}

	// Capturar transcrição da IA (output audio)
	if audioTrans, ok := serverContent["audioTranscription"].(map[string]interface{}); ok {
		if aiText, ok := audioTrans["text"].(string); ok && aiText != "" {
			log.Printf("💬 [NATIVE] EVA: %s", aiText)
			go s.saveTranscription(session.IdosoID, "assistant", aiText)
		}
	}
	// ========== FIM TRANSCRIÇÃO NATIVA ==========

	// Detectar quando idoso terminou de falar
	if turnComplete, ok := serverContent["turnComplete"].(bool); ok && turnComplete {
		log.Printf("🎙️ [Idoso terminou de falar]")
	}

	// Processar modelTurn (resposta da EVA)
	modelTurn, ok := serverContent["modelTurn"].(map[string]interface{})
	if !ok {
		return
	}

	parts, ok := modelTurn["parts"].([]interface{})
	if !ok {
		return
	}

	for i := range parts {
		partMap, ok := parts[i].(map[string]interface{})
		if !ok {
			continue
		}

		// Processar áudio da EVA
		if inlineData, ok := partMap["inlineData"].(map[string]interface{}); ok {
			mimeType, _ := inlineData["mimeType"].(string)
			audioB64, _ := inlineData["data"].(string)

			if strings.Contains(strings.ToLower(mimeType), "audio/pcm") && audioB64 != "" {
				audioData, err := base64.StdEncoding.DecodeString(audioB64)
				if err != nil {
					continue
				}

				session.WSConn.WriteMessage(websocket.BinaryMessage, audioData)
			}
		}

		// Processar function calls
		if fnCall, ok := partMap["functionCall"].(map[string]interface{}); ok {
			s.executeTool(session, fnCall)
		}
	}
}

func (s *SignalingServer) executeTool(session *WebSocketSession, fnCall map[string]interface{}) {
	name, _ := fnCall["name"].(string)
	args, _ := fnCall["args"].(map[string]interface{})

	switch name {
	case "alert_family":
		reason, _ := args["reason"].(string)
		log.Printf("🚨 Alerta enviado: %s", reason)

		if err := gemini.AlertFamily(s.db, s.pushService, session.IdosoID, reason); err != nil {
			log.Printf("❌ Erro ao enviar alerta")
		}

	case "confirm_medication":
		medication, _ := args["medication_name"].(string)
		log.Printf("💊 Medicamento confirmado: %s", medication)

		if err := gemini.ConfirmMedication(s.db, s.pushService, session.IdosoID, medication); err != nil {
			log.Printf("❌ Erro ao confirmar medicamento")
		}
	}
}

// 💾 saveTranscription salva a transcrição no banco de forma assíncrona
func (s *SignalingServer) saveTranscription(idosoID int64, role, content string) {
	// Formatar mensagem: [HH:MM:SS] ROLE: content
	timestamp := time.Now().Format("15:04:05")
	roleLabel := "IDOSO"
	if role == "assistant" {
		roleLabel = "EVA"
	}

	formattedMsg := fmt.Sprintf("[%s] %s: %s", timestamp, roleLabel, content)

	// Tentar atualizar registro ativo (últimos 5 minutos)
	updateQuery := `
		UPDATE historico_ligacoes 
		SET transcricao_completa = COALESCE(transcricao_completa, '') || E'\n' || $2
		WHERE id = (
			SELECT id 
			FROM historico_ligacoes
			WHERE idoso_id = $1 
			  AND fim_chamada IS NULL
			  AND inicio_chamada > NOW() - INTERVAL '5 minutes'
			ORDER BY inicio_chamada DESC 
			LIMIT 1
		)
		RETURNING id
	`

	var historyID int64
	err := s.db.QueryRow(updateQuery, idosoID, formattedMsg).Scan(&historyID)

	// Se não existe registro ativo, criar novo
	if err == sql.ErrNoRows {
		insertQuery := `
			INSERT INTO historico_ligacoes (
				agendamento_id, 
				idoso_id, 
				inicio_chamada,
				transcricao_completa
			)
			VALUES (
				(SELECT id FROM agendamentos WHERE idoso_id = $1 AND status IN ('agendado', 'em_andamento') ORDER BY data_hora_agendada DESC LIMIT 1),
				$1,
				CURRENT_TIMESTAMP,
				$2
			)
			RETURNING id
		`

		err = s.db.QueryRow(insertQuery, idosoID, formattedMsg).Scan(&historyID)
		if err != nil {
			log.Printf("⚠️ Erro ao criar histórico: %v", err)
			return
		}
		log.Printf("📝 Novo histórico criado: #%d para idoso %d", historyID, idosoID)
	} else if err != nil {
		log.Printf("⚠️ Erro ao atualizar transcrição: %v", err)
	}
}

func (s *SignalingServer) createSession(sessionID, cpf string, idosoID int64, conn *websocket.Conn) (*WebSocketSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)

	geminiClient, err := gemini.NewClient(ctx, s.cfg)
	if err != nil {
		cancel()
		return nil, err
	}

	instructions := buildInstructions(idosoID, s.db)
	if err := geminiClient.SendSetup(instructions, gemini.GetDefaultTools()); err != nil {
		cancel()
		geminiClient.Close()
		return nil, err
	}

	session := &WebSocketSession{
		ID:           sessionID,
		CPF:          cpf,
		IdosoID:      idosoID,
		WSConn:       conn,
		GeminiClient: geminiClient,
		ctx:          ctx,
		cancel:       cancel,
		lastActivity: time.Now(),
	}

	s.sessions.Store(sessionID, session)

	return session, nil
}

func (s *SignalingServer) cleanupSession(sessionID string) {
	val, ok := s.sessions.LoadAndDelete(sessionID)
	if !ok {
		return
	}

	session := val.(*WebSocketSession)
	session.cancel()

	if session.GeminiClient != nil {
		session.GeminiClient.Close()
	}

	// 🧠 ANALISAR CONVERSA AUTOMATICAMENTE
	go s.analyzeAndSaveConversation(session.IdosoID)
}

// analyzeAndSaveConversation analisa a conversa usando dados já no banco
func (s *SignalingServer) analyzeAndSaveConversation(idosoID int64) {
	log.Printf("🔍 [ANÁLISE] Iniciando análise para idoso %d", idosoID)

	// Buscar última transcrição sem fim_chamada
	query := `
		SELECT id, transcricao_completa
		FROM historico_ligacoes
		WHERE idoso_id = $1 
		  AND fim_chamada IS NULL
		  AND transcricao_completa IS NOT NULL
		  AND LENGTH(transcricao_completa) > 50
		ORDER BY inicio_chamada DESC
		LIMIT 1
	`

	var historyID int64
	var transcript string
	err := s.db.QueryRow(query, idosoID).Scan(&historyID, &transcript)
	if err == sql.ErrNoRows {
		log.Printf("⚠️ [ANÁLISE] Nenhuma transcrição encontrada para idoso %d", idosoID)
		return
	}
	if err != nil {
		log.Printf("❌ [ANÁLISE] Erro ao buscar transcrição: %v", err)
		return
	}

	log.Printf("📝 [ANÁLISE] Transcrição: %d caracteres", len(transcript))

	// Mostrar prévia
	preview := transcript
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	log.Printf("📄 [ANÁLISE] Prévia:\n%s", preview)

	log.Printf("🧠 [ANÁLISE] Enviando para Gemini API REST...")

	// Chamar análise do Gemini (REST API)
	analysis, err := gemini.AnalyzeConversation(s.cfg, transcript)
	if err != nil {
		log.Printf("❌ [ANÁLISE] Erro no Gemini: %v", err)
		return
	}

	log.Printf("✅ [ANÁLISE] Análise recebida!")
	log.Printf("   📊 Urgência: %s", analysis.UrgencyLevel)
	log.Printf("   😊 Humor: %s", analysis.MoodState)
	if analysis.ReportedPain {
		log.Printf("   🩺 Dor: %s (intensidade %d/10)", analysis.PainLocation, analysis.PainIntensity)
	}
	if analysis.EmergencySymptoms {
		log.Printf("   🚨 EMERGÊNCIA: %s", analysis.EmergencyType)
	}

	// Converter para JSON
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		log.Printf("❌ [ANÁLISE] Erro ao serializar: %v", err)
		return
	}

	log.Printf("💾 [ANÁLISE] Salvando no banco...")

	// Atualizar banco com análise NOS CAMPOS CORRETOS
	updateQuery := `
		UPDATE historico_ligacoes 
		SET 
			fim_chamada = CURRENT_TIMESTAMP,
			analise_gemini = $2::jsonb,
			urgencia = $3,
			sentimento = $4,
			transcricao_resumo = $5
		WHERE id = $1
	`

	result, err := s.db.Exec(
		updateQuery,
		historyID,
		string(analysisJSON),  // analise_gemini (JSON completo)
		analysis.UrgencyLevel, // urgencia
		analysis.MoodState,    // sentimento
		analysis.Summary,      // transcricao_resumo
	)

	if err != nil {
		log.Printf("❌ [ANÁLISE] Erro ao salvar: %v", err)
		return
	}

	rows, _ := result.RowsAffected()
	log.Printf("✅ [ANÁLISE] Salvo com sucesso! (%d linha atualizada)", rows)

	// 🚨 ALERTA CRÍTICO OU ALTO
	if analysis.UrgencyLevel == "CRITICO" || analysis.UrgencyLevel == "ALTO" {
		log.Printf("🚨 ALERTA DE URGÊNCIA: %s", analysis.UrgencyLevel)
		log.Printf("   Motivo: %s", analysis.RecommendedAction)
		log.Printf("   Preocupações: %v", analysis.KeyConcerns)

		alertMsg := fmt.Sprintf(
			"URGÊNCIA %s: %s. %s",
			analysis.UrgencyLevel,
			strings.Join(analysis.KeyConcerns, ", "),
			analysis.RecommendedAction,
		)

		err := gemini.AlertFamily(s.db, s.pushService, idosoID, alertMsg)
		if err != nil {
			log.Printf("❌ [ANÁLISE] Erro ao alertar família: %v", err)
		} else {
			log.Printf("✅ [ANÁLISE] Família alertada com sucesso!")
		}
	}
}

// getSentimentIntensity converte análise em escala 1-10
func getSentimentIntensity(analysis *gemini.ConversationAnalysis) int {
	intensity := 5 // neutro

	if analysis.EmergencySymptoms {
		return 10
	}

	if analysis.Depression {
		intensity = 8
	} else if analysis.MoodState == "triste" {
		intensity = 7
	} else if analysis.MoodState == "ansioso" {
		intensity = 6
	} else if analysis.MoodState == "feliz" {
		intensity = 3
	}

	if analysis.ReportedPain {
		intensity += analysis.PainIntensity / 3
	}

	if intensity > 10 {
		intensity = 10
	}

	return intensity
}

func (s *SignalingServer) cleanupDeadSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		var toDelete []string

		s.sessions.Range(func(key, value interface{}) bool {
			sessionID := key.(string)
			session := value.(*WebSocketSession)

			session.mu.RLock()
			inactive := now.Sub(session.lastActivity)
			session.mu.RUnlock()

			if inactive > 30*time.Minute {
				toDelete = append(toDelete, sessionID)
			}

			return true
		})

		for _, sessionID := range toDelete {
			s.cleanupSession(sessionID)
		}
	}
}

func (s *SignalingServer) getIdosoByCPF(cpf string) (*Idoso, error) {
	query := `
		SELECT id, nome, cpf, device_token, ativo, nivel_cognitivo
		FROM idosos 
		WHERE cpf = $1 AND ativo = true
	`

	var idoso Idoso
	err := s.db.QueryRow(query, cpf).Scan(
		&idoso.ID,
		&idoso.Nome,
		&idoso.CPF,
		&idoso.DeviceToken,
		&idoso.Ativo,
		&idoso.NivelCognitivo,
	)

	if err != nil {
		return nil, err
	}

	return &idoso, nil
}

func (s *SignalingServer) sendMessage(conn *websocket.Conn, msg ControlMessage) {
	data, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, data)
}

func (s *SignalingServer) sendError(conn *websocket.Conn, errMsg string) {
	s.sendMessage(conn, ControlMessage{
		Type:    "error",
		Error:   errMsg,
		Success: false,
	})
}

func buildInstructions(idosoID int64, db *sql.DB) string {
	// Buscar dados do idoso
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

	err := db.QueryRow(query, idosoID).Scan(
		&nome,
		&idade,
		&nivelCognitivo,
		&limitacoesAuditivas,
		&usaAparelhoAuditivo,
		&tomVoz,
	)

	if err != nil {
		// Fallback se der erro
		return `Você é a EVA, assistente de saúde virtual.
Fale em português brasileiro de forma carinhosa e clara.
Respostas curtas: 1-2 frases.`
	}

	// Buscar template do banco
	templateQuery := `
		SELECT template
		FROM prompt_templates
		WHERE nome = 'eva_base_v2' AND ativo = true
		LIMIT 1
	`

	var template string
	err = db.QueryRow(templateQuery).Scan(&template)
	if err != nil {
		// Fallback se não tiver template
		return fmt.Sprintf(`Você é a EVA, assistente de saúde virtual.
O idoso se chama %s, %d anos.
Nível cognitivo: %s
Tom de voz: %s
Fale de forma %s, clara e pausada.`, nome, idade, nivelCognitivo, tomVoz, tomVoz)
	}

	// Substituir variáveis Mustache
	instructions := strings.ReplaceAll(template, "{{nome_idoso}}", nome)
	instructions = strings.ReplaceAll(instructions, "{{idade}}", fmt.Sprintf("%d", idade))
	instructions = strings.ReplaceAll(instructions, "{{nivel_cognitivo}}", nivelCognitivo)
	instructions = strings.ReplaceAll(instructions, "{{tom_voz}}", tomVoz)

	// Processar condicionais
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

	// Limpar variáveis não usadas
	instructions = strings.ReplaceAll(instructions, "{{#primeira_interacao}}", "")
	instructions = strings.ReplaceAll(instructions, "{{/primeira_interacao}}", "")
	instructions = strings.ReplaceAll(instructions, "{{^primeira_interacao}}", "")
	instructions = strings.ReplaceAll(instructions, "{{taxa_adesao}}", "85")

	return instructions
}

func generateSessionID() string {
	return fmt.Sprintf("session-%d", time.Now().Unix())
}

type ControlMessage struct {
	Type      string `json:"type"`
	CPF       string `json:"cpf,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Success   bool   `json:"success,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Idoso struct {
	ID             int64
	Nome           string
	CPF            string
	DeviceToken    sql.NullString
	Ativo          bool
	NivelCognitivo string
}
