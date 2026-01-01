package push

import (
	"context"
	"fmt"
	"log"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

type FirebaseService struct {
	client *messaging.Client
	ctx    context.Context
}

type AlertResult struct {
	Success      bool
	MessageID    string
	Error        error
	SentAt       time.Time
	DeliveryType string // "push", "sms", "email", "call"
}

// NewFirebaseService inicializa o cliente Firebase com suporte a FCM
func NewFirebaseService(credentialsPath string) (*FirebaseService, error) {
	ctx := context.Background()

	opt := option.WithCredentialsFile(credentialsPath)
	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		return nil, fmt.Errorf("error initializing Firebase app: %w", err)
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting Messaging client: %w", err)
	}

	log.Println("✅ Firebase service initialized successfully")

	return &FirebaseService{
		client: client,
		ctx:    ctx,
	}, nil
}

// SendCallNotification dispara o sinal para o App "Ligar" e abrir o WebRTC
func (s *FirebaseService) SendCallNotification(deviceToken, sessionID, elderName string) error {
	if deviceToken == "" {
		return fmt.Errorf("device token is empty")
	}

	ttl := time.Duration(0)

	message := &messaging.Message{
		Token: deviceToken,
		Notification: &messaging.Notification{
			Title: "🤖 EVA está chamando",
			Body:  fmt.Sprintf("Olá %s, vamos conversar?", elderName),
		},
		Data: map[string]string{
			"type":      "incoming_call",
			"sessionId": sessionID,
			"action":    "START_VOICE_CALL",
			"priority":  "high",
			"timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		},
		Android: &messaging.AndroidConfig{
			Priority: "high",
			TTL:      &ttl,
			Notification: &messaging.AndroidNotification{
				Sound:        "default",
				Priority:     messaging.PriorityHigh,
				ChannelID:    "eva_calls",
				DefaultSound: true,
				ClickAction:  "OPEN_CALL_ACTIVITY",
			},
		},
	}

	response, err := s.client.Send(s.ctx, message)
	if err != nil {
		return fmt.Errorf("error sending call push: %w", err)
	}

	log.Printf("🚀 Ligação iniciada para %s (Session: %s): %s", elderName, sessionID, response)
	return nil
}

// SendAlertNotification envia alerta crítico para o cuidador
func (s *FirebaseService) SendAlertNotification(deviceToken, elderName, reason string) (*AlertResult, error) {
	if deviceToken == "" {
		return &AlertResult{
			Success:      false,
			Error:        fmt.Errorf("device token is empty"),
			SentAt:       time.Now(),
			DeliveryType: "push",
		}, fmt.Errorf("device token is empty")
	}

	message := &messaging.Message{
		Token: deviceToken,
		Notification: &messaging.Notification{
			Title: "⚠️ ALERTA CRÍTICO: EVA",
			Body:  fmt.Sprintf("%s precisa de ajuda: %s", elderName, reason),
		},
		Data: map[string]string{
			"type":      "emergency_alert",
			"reason":    reason,
			"priority":  "high",
			"timestamp": fmt.Sprintf("%d", time.Now().Unix()),
			"alert_id":  fmt.Sprintf("alert-%d", time.Now().UnixNano()),
		},
		Android: &messaging.AndroidConfig{
			Priority: "high",
			Notification: &messaging.AndroidNotification{
				Sound:        "alert",
				Priority:     messaging.PriorityHigh,
				ChannelID:    "eva_alerts",
				DefaultSound: true,
				Color:        "#FF0000",
			},
		},
	}

	response, err := s.client.Send(s.ctx, message)

	result := &AlertResult{
		Success:      err == nil,
		MessageID:    response,
		Error:        err,
		SentAt:       time.Now(),
		DeliveryType: "push",
	}

	if err != nil {
		log.Printf("❌ Erro ao enviar alerta de emergência: %v", err)
		return result, fmt.Errorf("error sending alert push: %w", err)
	}

	log.Printf("⚠️ Alerta de emergência enviado: %s", response)
	return result, nil
}

// SendAlertNotificationMultiple envia alertas para múltiplos tokens
func (s *FirebaseService) SendAlertNotificationMultiple(tokens []string, elderName, reason string) []*AlertResult {
	results := make([]*AlertResult, 0, len(tokens))

	for _, token := range tokens {
		result, err := s.SendAlertNotification(token, elderName, reason)
		if err != nil {
			log.Printf("❌ Falha ao enviar para token: %v", err)
		}
		results = append(results, result)
	}

	return results
}

// SendMedicationConfirmation confirma para o cuidador que o idoso tomou o remédio
func (s *FirebaseService) SendMedicationConfirmation(deviceToken, elderName, medicationName string) error {
	if deviceToken == "" {
		return fmt.Errorf("device token is empty")
	}

	message := &messaging.Message{
		Token: deviceToken,
		Notification: &messaging.Notification{
			Title: "✅ Medicamento Confirmado",
			Body:  fmt.Sprintf("%s tomou o remédio: %s", elderName, medicationName),
		},
		Data: map[string]string{
			"type":       "medication_confirmed",
			"medication": medicationName,
			"timestamp":  fmt.Sprintf("%d", time.Now().Unix()),
		},
		Android: &messaging.AndroidConfig{
			Priority: "normal",
			Notification: &messaging.AndroidNotification{
				Sound:        "default",
				ChannelID:    "eva_medications",
				DefaultSound: true,
				Color:        "#00FF00",
			},
		},
	}

	response, err := s.client.Send(s.ctx, message)
	if err != nil {
		return fmt.Errorf("error sending medication push: %w", err)
	}

	log.Printf("✅ Confirmação de medicação enviada: %s", response)
	return nil
}

// SendMissedCallAlert notifica o cuidador quando o idoso não atende uma chamada agendada
func (s *FirebaseService) SendMissedCallAlert(deviceToken, elderName string) error {
	if deviceToken == "" {
		return fmt.Errorf("device token is empty")
	}

	message := &messaging.Message{
		Token: deviceToken,
		Notification: &messaging.Notification{
			Title: "⚠️ Chamada Não Atendida",
			Body:  fmt.Sprintf("%s não atendeu a chamada programada da EVA. Verifique se está tudo bem.", elderName),
		},
		Data: map[string]string{
			"type":       "missed_call_alert",
			"elder_name": elderName,
			"priority":   "high",
			"timestamp":  fmt.Sprintf("%d", time.Now().Unix()),
		},
		Android: &messaging.AndroidConfig{
			Priority: "high",
			Notification: &messaging.AndroidNotification{
				Sound:        "alert",
				Priority:     messaging.PriorityHigh,
				ChannelID:    "eva_alerts",
				DefaultSound: true,
				Color:        "#FF0000",
			},
		},
	}

	response, err := s.client.Send(s.ctx, message)
	if err != nil {
		return fmt.Errorf("error sending missed call alert: %w", err)
	}

	log.Printf("📵 Alerta de chamada perdida enviado: %s", response)
	return nil
}

// ValidateToken verifica se um device token é válido
func (s *FirebaseService) ValidateToken(deviceToken string) bool {
	if deviceToken == "" {
		return false
	}

	// Tenta enviar uma mensagem de teste silenciosa
	message := &messaging.Message{
		Token: deviceToken,
		Data: map[string]string{
			"type": "token_validation",
		},
		Android: &messaging.AndroidConfig{
			Priority: "normal",
		},
	}

	response, err := s.client.Send(s.ctx, message)
	if err != nil {
		log.Printf("❌ ValidateToken failed for token %s...: %v", deviceToken[:10], err)
		return false
	}
	_ = response // Ignorar response ID
	return true
}

// GetClient e GetContext para flexibilidade em outros módulos
func (s *FirebaseService) GetClient() *messaging.Client { return s.client }
func (s *FirebaseService) GetContext() context.Context  { return s.ctx }

// IsInvalidTokenError verifica se o erro retornado pelo Firebase indica que o token é inválido
func IsInvalidTokenError(err error) bool {
	if messaging.IsRegistrationTokenNotRegistered(err) || messaging.IsSenderIDMismatch(err) {
		return true
	}
	return false
}
