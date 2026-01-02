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
			log.Printf("💬 [TRANSCRICAO] EVA: %s", aiText)
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

				log.Printf("🎶 [AUDIO] Enviando %d bytes de áudio para o cliente", len(audioData))
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

	instructions := BuildInstructions(idosoID, s.db)
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

func BuildInstructions(idosoID int64, db *sql.DB) string {
	// 1. QUERY EXAUSTIVA: Recuperar TODOS os campos relevantes da tabela 'idosos'
	// Esquema fornecido: id, nome, data_nascimento, telefone, cpf, foto_url, intro_audio_url,
	// nivel_cognitivo, limitacoes_auditivas, usa_aparelho_auditivo, limitacoes_visuais,
	// mobilidade, tom_voz, preferencia_horario_ligacao, timezone, ganho_audio_entrada,
	// ganho_audio_saida, ambiente_ruidoso, familiar_principal, contato_emergencia,
	// medico_responsavel, medicamentos_atuais, condicoes_medicas, sentimento,
	// agendamentos_pendentes, notas_gerais, ativo, criado_em, atualizado_em,
	// endereco, medicamentos_regulares, device_token, device_token_valido, device_token_atualizado_em

	query := `
		SELECT 
			nome, 
			EXTRACT(YEAR FROM AGE(data_nascimento)) as idade,
			nivel_cognitivo, 
			limitacoes_auditivas, 
			usa_aparelho_auditivo, 
			limitacoes_visuais,
			mobilidade,
			tom_voz,
			preferencia_horario_ligacao,
			ambiente_ruidoso,
			familiar_principal, 
			contato_emergencia, 
			medico_responsavel,
			medicamentos_atuais,
			medicamentos_regulares,
			condicoes_medicas,
			sentimento,
			notas_gerais,
			endereco
		FROM idosos 
		WHERE id = $1
	`

	var nome, nivelCognitivo, tomVoz, mobilidade string
	var idade int
	var limitacoesAuditivas, usaAparelhoAuditivo, ambienteRuidoso bool

	// Campos que podem ser NULL (usando NullString para segurança)
	var limitacoesVisuais, preferenciaHorario, familiarPrincipal, contatoEmergencia, medicoResponsavel sql.NullString
	var medicamentosAtuais, medicamentosRegulares, condicoesMedicas, sentimento, notasGerais, endereco sql.NullString

	err := db.QueryRow(query, idosoID).Scan(
		&nome,
		&idade,
		&nivelCognitivo,
		&limitacoesAuditivas,
		&usaAparelhoAuditivo,
		&limitacoesVisuais,
		&mobilidade,
		&tomVoz,
		&preferenciaHorario,
		&ambienteRuidoso,
		&familiarPrincipal,
		&contatoEmergencia,
		&medicoResponsavel,
		&medicamentosAtuais,
		&medicamentosRegulares,
		&condicoesMedicas,
		&sentimento,
		&notasGerais,
		&endereco,
	)

	if err != nil {
		log.Printf("❌ [BuildInstructions] ERRO CRÍTICO ao buscar dados: %v", err)
		// Fallback mínimo
		return "Você é a EVA, assistente de saúde virtual. Fale em português de forma clara."
	}

	// 🔍 DEBUG EXAUSTIVO DOS DADOS RECUPERADOS
	log.Printf("📋 [DADOS PACIENTE] Nome: %s, Idade: %d", nome, idade)
	log.Printf("   💊 Meds Atuais: %s", getString(medicamentosAtuais, "Nenhum"))
	log.Printf("   💊 Meds Regulares: %s", getString(medicamentosRegulares, "Nenhum"))
	log.Printf("   🏥 Condições: %s", getString(condicoesMedicas, "Nenhuma"))

	// 2. Buscar Template Base
	templateQuery := `SELECT template FROM prompt_templates WHERE nome = 'eva_base_v2' AND ativo = true LIMIT 1`
	var template string
	if err := db.QueryRow(templateQuery).Scan(&template); err != nil {
		log.Printf("⚠️ Template não encontrado, usando padrão.")
		template = `Você é a EVA, assistente de saúde virtual para {{nome_idoso}}.`
	}

	// 3. Montar "Dossiê do Paciente" (Texto Completo)
	// Isso garante que NENHUMA informação seja perdida, independente do template

	dossier := fmt.Sprintf("\n\n📍 --- FICHA COMPLETA DO PACIENTE (INFORMAÇÃO CONFIDENCIAL) ---\n")
	dossier += fmt.Sprintf("NOME: %s\n", nome)
	dossier += fmt.Sprintf("IDADE: %d anos\n", idade)
	dossier += fmt.Sprintf("ENDEREÇO: %s\n", getString(endereco, "Não completado"))

	dossier += "\n🏥 --- SAÚDE E CONDIÇÕES ---\n"
	dossier += fmt.Sprintf("Nível Cognitivo: %s\n", nivelCognitivo)
	dossier += fmt.Sprintf("Mobilidade: %s\n", mobilidade)
	dossier += fmt.Sprintf("Limitações Auditivas: %v (Usa Aparelho: %v)\n", limitacoesAuditivas, usaAparelhoAuditivo)
	dossier += fmt.Sprintf("Limitações Visuais: %s\n", getString(limitacoesVisuais, "Nenhuma"))
	dossier += fmt.Sprintf("Condições Médicas: %s\n", getString(condicoesMedicas, "Nenhuma registrada"))

	dossier += "\n💊 --- MEDICAMENTOS (IMPORTANTE) ---\n"
	medsA := getString(medicamentosAtuais, "")
	medsR := getString(medicamentosRegulares, "")
	if medsA == "" && medsR == "" {
		dossier += "Nenhum medicamento registrado no sistema.\n"
	} else {
		if medsA != "" {
			dossier += fmt.Sprintf("Atuais: %s\n", medsA)
		}
		if medsR != "" {
			dossier += fmt.Sprintf("Regulares: %s\n", medsR)
		}
		dossier += "INSTRUCÃO: Se o paciente perguntar o que deve tomar, consulte esta lista.\n"
	}

	dossier += "\n📞 --- REDE DE APOIO ---\n"
	dossier += fmt.Sprintf("Familiar: %s\n", getString(familiarPrincipal, "Não informado"))
	dossier += fmt.Sprintf("Emergência: %s\n", getString(contatoEmergencia, "Não informado"))
	dossier += fmt.Sprintf("Médico: %s\n", getString(medicoResponsavel, "Não informado"))

	dossier += "\n📝 --- OUTRAS NOTAS ---\n"
	dossier += fmt.Sprintf("Notas Gerais: %s\n", getString(notasGerais, ""))
	dossier += fmt.Sprintf("Preferência Horário: %s\n", getString(preferenciaHorario, "Indiferente"))
	dossier += fmt.Sprintf("Ambiente Ruidoso: %v\n", ambienteRuidoso)
	dossier += fmt.Sprintf("Tom de Voz Ideal: %s\n", tomVoz)
	dossier += "--------------------------------------------------------\n"

	// 4. Substituições no Template (para manter compatibilidade com tags existentes)
	instructions := template
	instructions = strings.ReplaceAll(instructions, "{{nome_idoso}}", nome)
	instructions = strings.ReplaceAll(instructions, "{{idade}}", fmt.Sprintf("%d", idade))
	instructions = strings.ReplaceAll(instructions, "{{nivel_cognitivo}}", nivelCognitivo)
	instructions = strings.ReplaceAll(instructions, "{{tom_voz}}", tomVoz)

	// Tags de dados (caso o template as use)
	instructions = strings.ReplaceAll(instructions, "{{medicamentos}}", medsA+" "+medsR)
	instructions = strings.ReplaceAll(instructions, "{{condicoes_medicas}}", getString(condicoesMedicas, ""))

	// Limpar tags condicionais não usadas
	tags := []string{"{{#limitacoes_auditivas}}", "{{/limitacoes_auditivas}}", "{{#usa_aparelho_auditivo}}", "{{/usa_aparelho_auditivo}}", "{{#primeira_interacao}}", "{{/primeira_interacao}}", "{{^primeira_interacao}}", "{{taxa_adesao}}"}
	for _, tag := range tags {
		instructions = strings.ReplaceAll(instructions, tag, "")
	}

	// 5. ANEXAR DOSSIÊ AO FINAL (Garantia Absoluta)
	finalInstructions := instructions + dossier

	log.Printf("✅ [BuildInstructions] Instruções finais geradas (%d chars)", len(finalInstructions))
	return finalInstructions
}

// Helper seguro para NullString
func getString(ns sql.NullString, def string) string {
	if ns.Valid {
		return ns.String
	}
	return def
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
