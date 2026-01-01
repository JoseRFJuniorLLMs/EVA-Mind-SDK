package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"eva-mind/internal/config"
	"eva-mind/internal/database"
	"eva-mind/internal/gemini"
	"eva-mind/internal/push"
	"eva-mind/internal/scheduler"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
)

type SignalingServer struct {
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
	lastActivity time.Time
	audioCount   int64
}

var (
	db              *database.DB
	pushService     *push.FirebaseService
	signalingServer *SignalingServer
	startTime       time.Time
)

func NewSignalingServer(cfg *config.Config, db *database.DB, pushService *push.FirebaseService) *SignalingServer {
	return &SignalingServer{
		upgrader: websocket.Upgrader{
			CheckOrigin:     func(r *http.Request) bool { return true },
			ReadBufferSize:  8192,
			WriteBufferSize: 8192,
		},
		clients:     make(map[string]*PCMClient),
		cfg:         cfg,
		pushService: pushService,
		db:          db,
	}
}

func main() {
	startTime = time.Now()
	log.Printf("🚀 EVA-Mind 2026-1")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("❌ Config error: %v", err)
	}

	db, err = database.NewDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("❌ DB error: %v", err)
	}
	defer db.Close()

	pushService, err = push.NewFirebaseService(cfg.FirebaseCredentialsPath)
	if err != nil {
		log.Printf("⚠️ Firebase warning: %v", err)
	} else {
		log.Printf("✅ Firebase initialized")
	}

	signalingServer = NewSignalingServer(cfg, db, pushService)

	sch, err := scheduler.NewScheduler(cfg, db.GetConnection())
	if err != nil {
		log.Printf("⚠️ Scheduler error: %v", err)
	} else {
		go sch.Start(context.Background())
		log.Printf("✅ Scheduler started")
	}

	router := mux.NewRouter()
	router.HandleFunc("/wss", signalingServer.HandleWebSocket)
	router.HandleFunc("/ws/pcm", signalingServer.HandleWebSocket)

	api := router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/stats", statsHandler).Methods("GET")
	api.HandleFunc("/health", healthCheckHandler).Methods("GET")

	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./web")))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("✅ Server ready on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, corsMiddleware(router)))
}

func (s *SignalingServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	log.Printf("🌐 Nova conexão de %s", r.RemoteAddr)

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("❌ Upgrade error: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := &PCMClient{
		Conn:         conn,
		SendCh:       make(chan []byte, 256), // Buffer maior
		ctx:          ctx,
		cancel:       cancel,
		lastActivity: time.Now(),
	}

	go s.handleClientSend(client)
	go s.monitorClientActivity(client)
	s.handleClientMessages(client)
}

func (s *SignalingServer) handleClientMessages(client *PCMClient) {
	defer s.cleanupClient(client)

	for {
		msgType, message, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("⚠️ Unexpected close: %v", err)
			}
			break
		}

		client.lastActivity = time.Now()

		if msgType == websocket.TextMessage {
			var data map[string]interface{}
			if err := json.Unmarshal(message, &data); err != nil {
				log.Printf("❌ JSON error: %v", err)
				continue
			}

			switch data["type"] {
			case "register":
				s.registerClient(client, data)
			case "start_call":
				if client.CPF == "" {
					s.sendJSON(client, map[string]string{"type": "error", "message": "Register first"})
					continue
				}
				s.startGeminiSession(client)
			case "hangup":
				log.Printf("📴 Hangup from %s", client.CPF)
				return
			}
		}

		if msgType == websocket.BinaryMessage && client.active {
			client.audioCount++

			// Log apenas a cada 50 chunks para reduzir verbosidade
			if client.audioCount%50 == 0 {
				log.Printf("🎤 [%s] Áudio chunk #%d (%d bytes)", client.CPF, client.audioCount, len(message))
			}

			if client.GeminiClient != nil {
				client.GeminiClient.SendAudio(message)
			}
		}
	}
}

func (s *SignalingServer) registerClient(client *PCMClient, data map[string]interface{}) {
	cpf, _ := data["cpf"].(string)
	log.Printf("📝 Registrando CPF: %s", cpf)

	idoso, err := s.db.GetIdosoByCPF(cpf)
	if err != nil {
		log.Printf("❌ CPF não encontrado: %s", cpf)
		s.sendJSON(client, map[string]string{"type": "error", "message": "CPF não cadastrado"})
		return
	}

	client.CPF = idoso.CPF
	client.IdosoID = idoso.ID

	s.mu.Lock()
	s.clients[idoso.CPF] = client
	s.mu.Unlock()

	s.sendJSON(client, map[string]string{"type": "registered"})
	log.Printf("✅ Cliente registrado: %s", cpf)
}

func (s *SignalingServer) startGeminiSession(client *PCMClient) {
	log.Printf("🤖 Iniciando Gemini para %s", client.CPF)

	gemClient, err := gemini.NewClient(client.ctx, s.cfg)
	if err != nil {
		log.Printf("❌ Gemini error: %v", err)
		s.sendJSON(client, map[string]string{"type": "error", "message": "IA error"})
		return
	}

	client.GeminiClient = gemClient

	instructions := s.buildPrompt(client.IdosoID)
	tools := gemini.GetDefaultTools()

	client.GeminiClient.SendSetup(instructions, tools)
	go s.listenGemini(client)

	client.active = true
	s.sendJSON(client, map[string]string{"type": "session_created", "status": "ready"})
	log.Printf("✅ Sessão criada: %s", client.CPF)
}

func (s *SignalingServer) buildPrompt(idosoID int64) string {
	var nome, tom string
	s.db.GetConnection().QueryRow("SELECT nome, tom_voz FROM idosos WHERE id = $1", idosoID).Scan(&nome, &tom)

	if tom == "" {
		tom = "calmo e acolhedor"
	}
	return fmt.Sprintf("Você é a EVA, assistente virtual para idosos. Ajude o(a) %s. Use tom %s.", nome, tom)
}

func (s *SignalingServer) listenGemini(client *PCMClient) {
	log.Printf("👂 Listener iniciado: %s", client.CPF)

	for client.active {
		resp, err := client.GeminiClient.ReadResponse()
		if err != nil {
			if client.active {
				log.Printf("⚠️ Gemini read error: %v", err)
			}
			continue
		}
		s.processGeminiResponse(client, resp)
	}

	log.Printf("📚 Listener finalizado: %s", client.CPF)
}

func (s *SignalingServer) processGeminiResponse(client *PCMClient, resp map[string]interface{}) {
	serverContent, ok := resp["serverContent"].(map[string]interface{})
	if !ok {
		return
	}

	modelTurn, _ := serverContent["modelTurn"].(map[string]interface{})
	parts, _ := modelTurn["parts"].([]interface{})

	audioCount := 0
	for _, part := range parts {
		p, ok := part.(map[string]interface{})
		if !ok {
			continue
		}

		if data, hasData := p["inlineData"]; hasData {
			b64, _ := data.(map[string]interface{})["data"].(string)
			audio, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				continue
			}

			select {
			case client.SendCh <- audio:
				audioCount++
			default:
				log.Printf("⚠️ Canal cheio, dropando áudio")
			}
		}
	}

	if audioCount > 0 {
		log.Printf("✅ [%s] %d áudios enfileirados", client.CPF, audioCount)
	}
}

func (s *SignalingServer) handleClientSend(client *PCMClient) {
	sentCount := 0

	for {
		select {
		case <-client.ctx.Done():
			return
		case audio := <-client.SendCh:
			sentCount++

			// Log apenas a cada 20 envios
			if sentCount%20 == 0 {
				log.Printf("📤 [%s] Sent #%d", client.CPF, sentCount)
			}

			client.mu.Lock()
			err := client.Conn.WriteMessage(websocket.BinaryMessage, audio)
			client.mu.Unlock()

			if err != nil {
				log.Printf("❌ Send error: %v", err)
				return
			}
		}
	}
}

func (s *SignalingServer) monitorClientActivity(client *PCMClient) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-client.ctx.Done():
			return
		case <-ticker.C:
			if time.Since(client.lastActivity) > 5*time.Minute {
				log.Printf("⏰ Timeout inativo: %s", client.CPF)
				client.cancel()
				return
			}
		}
	}
}

func (s *SignalingServer) cleanupClient(client *PCMClient) {
	log.Printf("🧹 Cleanup: %s", client.CPF)

	client.cancel()

	s.mu.Lock()
	delete(s.clients, client.CPF)
	s.mu.Unlock()

	client.Conn.Close()

	if client.GeminiClient != nil {
		client.GeminiClient.Close()
	}

	log.Printf("✅ Desconectado: %s", client.CPF)
}

func (s *SignalingServer) sendJSON(c *PCMClient, v interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Conn.WriteJSON(v)
}

func (s *SignalingServer) GetActiveClientsCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// --- API HANDLERS ---

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	dbStatus := false
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := db.GetConnection().PingContext(ctx); err == nil {
			dbStatus = true
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"active_clients": signalingServer.GetActiveClientsCount(),
		"uptime":         time.Since(startTime).String(),
		"db_status":      dbStatus,
	})
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := "healthy"
	httpStatus := http.StatusOK

	if err := db.GetConnection().Ping(); err != nil {
		status = "unhealthy"
		httpStatus = http.StatusServiceUnavailable
	}

	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}
