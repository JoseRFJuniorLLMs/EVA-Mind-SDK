package email

import (
	"eva-mind/internal/config"
	"fmt"

	"gopkg.in/gomail.v2"
)

type EmailService struct {
	cfg    *config.Config
	dialer *gomail.Dialer
}

// NewEmailService cria uma nova instância do serviço de email
func NewEmailService(cfg *config.Config) (*EmailService, error) {
	if cfg.SMTPUsername == "" || cfg.SMTPPassword == "" {
		return nil, fmt.Errorf("SMTP credentials not configured")
	}

	dialer := gomail.NewDialer(
		cfg.SMTPHost,
		cfg.SMTPPort,
		cfg.SMTPUsername,
		cfg.SMTPPassword,
	)

	return &EmailService{
		cfg:    cfg,
		dialer: dialer,
	}, nil
}

// SendEmail envia um email com HTML
func (s *EmailService) SendEmail(to, subject, htmlBody string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", fmt.Sprintf("%s <%s>", s.cfg.SMTPFromName, s.cfg.SMTPFromEmail))
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", htmlBody)

	if err := s.dialer.DialAndSend(m); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}
