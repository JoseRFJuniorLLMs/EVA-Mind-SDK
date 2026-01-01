package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	// Server
	Port        string
	Environment string
	MetricsPort string

	// Database
	DatabaseURL string

	// Twilio (para fallback SMS e chamadas)
	ServiceDomain     string
	TwilioAccountSID  string
	TwilioAuthToken   string
	TwilioPhoneNumber string

	// Google/Gemini
	GoogleAPIKey        string
	ModelID             string
	GeminiAnalysisModel string

	// Scheduler
	SchedulerInterval int
	MaxRetries        int

	// Firebase
	FirebaseCredentialsPath string

	// Alert System
	AlertRetryInterval   int  // Intervalo entre tentativas de reenvio (minutos)
	AlertEscalationTime  int  // Tempo até escalonamento (minutos)
	EnableSMSFallback    bool // Habilitar SMS como fallback
	EnableEmailFallback  bool // Habilitar Email como fallback
	EnableCallFallback   bool // Habilitar ligação como fallback
	CriticalAlertTimeout int  // Timeout para alertas críticos (minutos)

	// SMTP Configuration
	SMTPHost      string
	SMTPPort      int
	SMTPUsername  string
	SMTPPassword  string
	SMTPFromName  string
	SMTPFromEmail string
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("ℹ️  Info: Ficheiro .env não encontrado ou não pôde ser carregado. Lendo variáveis de ambiente do sistema.")
	}

	return &Config{
		// Server
		Port:        getEnvWithDefault("PORT", "8080"),
		Environment: getEnvWithDefault("ENVIRONMENT", "development"),
		MetricsPort: getEnvWithDefault("METRICS_PORT", "9090"),

		// Database
		DatabaseURL: os.Getenv("DATABASE_URL"),

		// Twilio
		TwilioAccountSID:  os.Getenv("TWILIO_ACCOUNT_SID"),
		TwilioAuthToken:   os.Getenv("TWILIO_AUTH_TOKEN"),
		TwilioPhoneNumber: os.Getenv("TWILIO_PHONE_NUMBER"),

		// Google/Gemini
		GoogleAPIKey:        os.Getenv("GOOGLE_API_KEY"),
		ModelID:             getEnvWithDefault("MODEL_ID", "gemini-2.5-flash-native-audio-preview-12-2025"),
		GeminiAnalysisModel: getEnvWithDefault("GEMINI_ANALYSIS_MODEL", "gemini-2.5-flash"),

		// Scheduler
		SchedulerInterval: getEnvInt("SCHEDULER_INTERVAL", 1),
		MaxRetries:        getEnvInt("MAX_RETRIES", 3),

		// Firebase
		FirebaseCredentialsPath: os.Getenv("FIREBASE_CREDENTIALS_PATH"),

		// Alert System
		AlertRetryInterval:   getEnvInt("ALERT_RETRY_INTERVAL", 5),
		AlertEscalationTime:  getEnvInt("ALERT_ESCALATION_TIME", 5),
		EnableSMSFallback:    getEnvBool("ENABLE_SMS_FALLBACK", false),
		EnableEmailFallback:  getEnvBool("ENABLE_EMAIL_FALLBACK", true),
		EnableCallFallback:   getEnvBool("ENABLE_CALL_FALLBACK", false),
		CriticalAlertTimeout: getEnvInt("CRITICAL_ALERT_TIMEOUT", 5),

		// SMTP
		SMTPHost:      getEnvWithDefault("SMTP_HOST", "smtp.gmail.com"),
		SMTPPort:      getEnvInt("SMTP_PORT", 587),
		SMTPUsername:  os.Getenv("SMTP_USERNAME"),
		SMTPPassword:  os.Getenv("SMTP_PASSWORD"),
		SMTPFromName:  getEnvWithDefault("SMTP_FROM_NAME", "EVA - Assistente Virtual"),
		SMTPFromEmail: getEnvWithDefault("SMTP_FROM_EMAIL", "web2ajax@gmail.com"),
	}, nil
}

func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intValue int
		if _, err := fmt.Sscanf(value, "%d", &intValue); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}

// Validate valida se todas as configurações obrigatórias estão presentes
func (c *Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	if c.GoogleAPIKey == "" {
		return fmt.Errorf("GOOGLE_API_KEY is required")
	}

	if c.FirebaseCredentialsPath == "" {
		return fmt.Errorf("FIREBASE_CREDENTIALS_PATH is required")
	}

	// Verificar se fallbacks estão habilitados mas sem credenciais
	if c.EnableSMSFallback && (c.TwilioAccountSID == "" || c.TwilioAuthToken == "") {
		log.Println("⚠️  SMS fallback habilitado mas credenciais Twilio não configuradas")
	}

	if c.EnableEmailFallback && (c.SMTPUsername == "" || c.SMTPPassword == "") {
		log.Println("⚠️  Email fallback habilitado mas credenciais SMTP não configuradas")
	}

	return nil
}
