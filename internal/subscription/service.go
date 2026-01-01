package subscription

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

// PlanFeatures define as features disponíveis por plano
var PlanFeatures = map[string]map[string]bool{
	"livre": {
		"interface_acessivel":     true,
		"cadastro_idoso":          true,
		"historico_chamadas":      true,
		"ligar_agora_manual":      true,
		"lembretes_automaticos":   false,
		"confirmacao_medicacao":   false,
		"personalizacao_audio":    false,
		"alertas_nao_atendeu":     false,
		"deteccao_emergencias":    false,
		"monitoramento_tempo_real": false,
		"relatorios_detalhados":   false,
		"ia_avancada":             false,
		"api_integracao":          false,
		"suporte_prioritario":     false,
	},
	"essencial": {
		"interface_acessivel":     true,
		"cadastro_idoso":          true,
		"historico_chamadas":      true,
		"ligar_agora_manual":      true,
		"lembretes_automaticos":   true,
		"confirmacao_medicacao":   true,
		"personalizacao_audio":    true,
		"alertas_nao_atendeu":     true,
		"deteccao_emergencias":    false,
		"monitoramento_tempo_real": false,
		"relatorios_detalhados":   false,
		"ia_avancada":             false,
		"api_integracao":          false,
		"suporte_prioritario":     false,
	},
	"familia_plus": {
		"interface_acessivel":     true,
		"cadastro_idoso":          true,
		"historico_chamadas":      true,
		"ligar_agora_manual":      true,
		"lembretes_automaticos":   true,
		"confirmacao_medicacao":   true,
		"personalizacao_audio":    true,
		"alertas_nao_atendeu":     true,
		"deteccao_emergencias":    true,
		"monitoramento_tempo_real": true,
		"relatorios_detalhados":   true,
		"ia_avancada":             true,
		"api_integracao":          false,
		"suporte_prioritario":     false,
	},
	"profissional": {
		"interface_acessivel":     true,
		"cadastro_idoso":          true,
		"historico_chamadas":      true,
		"ligar_agora_manual":      true,
		"lembretes_automaticos":   true,
		"confirmacao_medicacao":   true,
		"personalizacao_audio":    true,
		"alertas_nao_atendeu":     true,
		"deteccao_emergencias":    true,
		"monitoramento_tempo_real": true,
		"relatorios_detalhados":   true,
		"ia_avancada":             true,
		"idosos_ilimitados":       true,
		"integracao_sensores":     true,
		"lembretes_consultas":     true,
		"hipaa_ready":             true,
		"api_integracao":          true,
		"suporte_prioritario":     true,
	},
}

// Subscription representa uma assinatura na tabela assinaturas_entidade
type Subscription struct {
	ID              int
	EntityName      string
	Status          string
	PlanID          string
	NextBillingDate *time.Time
	MinutesLimit    int
	MinutesConsumed int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// SubscriptionService gerencia operações de assinaturas
type SubscriptionService struct {
	db *sql.DB
}

// NewSubscriptionService cria uma nova instância do serviço
func NewSubscriptionService(db *sql.DB) *SubscriptionService {
	return &SubscriptionService{db: db}
}

// GetActiveSubscription busca a assinatura ativa de uma entidade
func (s *SubscriptionService) GetActiveSubscription(entityName string) (*Subscription, error) {
	query := `
		SELECT id, entidade_nome, status, plano_id, data_proxima_cobranca,
		       limite_minutos, minutos_consumidos, criado_em, atualizado_em
		FROM assinaturas_entidade
		WHERE entidade_nome = $1 AND status = 'ativo'
		LIMIT 1
	`

	var sub Subscription
	var nextBilling sql.NullTime

	err := s.db.QueryRow(query, entityName).Scan(
		&sub.ID,
		&sub.EntityName,
		&sub.Status,
		&sub.PlanID,
		&nextBilling,
		&sub.MinutesLimit,
		&sub.MinutesConsumed,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("assinatura não encontrada para entidade: %s", entityName)
		}
		return nil, fmt.Errorf("erro ao buscar assinatura: %w", err)
	}

	if nextBilling.Valid {
		sub.NextBillingDate = &nextBilling.Time
	}

	return &sub, nil
}

// CheckFeature verifica se uma entidade tem acesso a uma feature específica
func (s *SubscriptionService) CheckFeature(entityName, feature string) (bool, error) {
	sub, err := s.GetActiveSubscription(entityName)
	if err != nil {
		return false, err
	}

	features, exists := PlanFeatures[sub.PlanID]
	if !exists {
		log.Printf("⚠️ Plano desconhecido: %s", sub.PlanID)
		return false, fmt.Errorf("plano desconhecido: %s", sub.PlanID)
	}

	hasFeature, exists := features[feature]
	if !exists {
		log.Printf("⚠️ Feature desconhecida: %s", feature)
		return false, nil
	}

	return hasFeature, nil
}

// GetPlanFeatures retorna todas as features de um plano
func (s *SubscriptionService) GetPlanFeatures(planID string) (map[string]bool, error) {
	features, exists := PlanFeatures[planID]
	if !exists {
		return nil, fmt.Errorf("plano desconhecido: %s", planID)
	}

	return features, nil
}

// ConsumeMinutes registra o consumo de minutos
func (s *SubscriptionService) ConsumeMinutes(entityName string, minutes int) error {
	sub, err := s.GetActiveSubscription(entityName)
	if err != nil {
		return err
	}

	newConsumed := sub.MinutesConsumed + minutes
	if newConsumed > sub.MinutesLimit {
		return fmt.Errorf("limite de minutos excedido: %d/%d", newConsumed, sub.MinutesLimit)
	}

	query := `
		UPDATE assinaturas_entidade
		SET minutos_consumidos = $1, atualizado_em = CURRENT_TIMESTAMP
		WHERE id = $2
	`

	_, err = s.db.Exec(query, newConsumed, sub.ID)
	if err != nil {
		return fmt.Errorf("erro ao atualizar consumo: %w", err)
	}

	log.Printf("✅ Consumo registrado: %s - %d minutos (total: %d/%d)",
		entityName, minutes, newConsumed, sub.MinutesLimit)

	return nil
}

// GetUsageStats retorna estatísticas de uso
func (s *SubscriptionService) GetUsageStats(entityName string) (consumed, limit int, err error) {
	sub, err := s.GetActiveSubscription(entityName)
	if err != nil {
		return 0, 0, err
	}

	return sub.MinutesConsumed, sub.MinutesLimit, nil
}

// GetSubscriptionByID busca assinatura por ID
func (s *SubscriptionService) GetSubscriptionByID(id int) (*Subscription, error) {
	query := `
		SELECT id, entidade_nome, status, plano_id, data_proxima_cobranca,
		       limite_minutos, minutos_consumidos, criado_em, atualizado_em
		FROM assinaturas_entidade
		WHERE id = $1
	`

	var sub Subscription
	var nextBilling sql.NullTime

	err := s.db.QueryRow(query, id).Scan(
		&sub.ID,
		&sub.EntityName,
		&sub.Status,
		&sub.PlanID,
		&nextBilling,
		&sub.MinutesLimit,
		&sub.MinutesConsumed,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("assinatura não encontrada: ID %d", id)
		}
		return nil, fmt.Errorf("erro ao buscar assinatura: %w", err)
	}

	if nextBilling.Valid {
		sub.NextBillingDate = &nextBilling.Time
	}

	return &sub, nil
}
