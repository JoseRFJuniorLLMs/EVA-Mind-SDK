package workers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// PredictionWorker prediz emergências
type PredictionWorker struct {
	db *sql.DB
}

// NewPredictionWorker cria um novo worker de predições
func NewPredictionWorker(db *sql.DB) *PredictionWorker {
	return &PredictionWorker{db: db}
}

// Name retorna o nome do worker
func (pw *PredictionWorker) Name() string {
	return "Emergency Predictor"
}

// Interval retorna o intervalo de execução (12 horas)
func (pw *PredictionWorker) Interval() time.Duration {
	return 12 * time.Hour
}

// Run executa a predição de emergências
func (pw *PredictionWorker) Run(ctx context.Context) error {
	log.Println("🔮 Iniciando predição de emergências...")

	// Buscar todos os idosos ativos
	idosos, err := pw.getActiveIdosos(ctx)
	if err != nil {
		return fmt.Errorf("erro ao buscar idosos: %w", err)
	}

	log.Printf("📊 Analisando riscos para %d idoso(s)...", len(idosos))

	totalPredicoes := 0
	for _, idosoID := range idosos {
		// Predizer depressão
		if pred, err := pw.predictDepression(ctx, idosoID); err == nil && pred != nil {
			if err := pw.savePrediction(ctx, pred); err == nil {
				totalPredicoes++
			}
		}

		// Predizer confusão mental
		if pred, err := pw.predictConfusion(ctx, idosoID); err == nil && pred != nil {
			if err := pw.savePrediction(ctx, pred); err == nil {
				totalPredicoes++
			}
		}

		// Predizer risco de queda (baseado em padrões)
		if pred, err := pw.predictFallRisk(ctx, idosoID); err == nil && pred != nil {
			if err := pw.savePrediction(ctx, pred); err == nil {
				totalPredicoes++
			}
		}
	}

	log.Printf("✅ Predição concluída: %d predição(ões) gerada(s)", totalPredicoes)
	return nil
}

// getActiveIdosos retorna lista de idosos ativos
func (pw *PredictionWorker) getActiveIdosos(ctx context.Context) ([]int, error) {
	query := `SELECT id FROM idosos WHERE ativo = true`

	rows, err := pw.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var idosos []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			continue
		}
		idosos = append(idosos, id)
	}

	return idosos, nil
}

// EmergencyPrediction representa uma predição de emergência
type EmergencyPrediction struct {
	IdosoID              int
	TipoEmergencia       string
	Probabilidade        float64
	NivelRisco           string
	FatoresContribuintes []string
	SinaisDetectados     map[string]interface{}
	Recomendacoes        []string
}

// predictDepression prediz risco de depressão
func (pw *PredictionWorker) predictDepression(ctx context.Context, idosoID int) (*EmergencyPrediction, error) {
	query := `
		SELECT 
			COUNT(CASE WHEN sentimento_geral IN ('triste', 'apatico') THEN 1 END) as sentimentos_negativos,
			COUNT(*) as total_ligacoes,
			AVG(CASE WHEN sentimento_geral IN ('triste', 'apatico') THEN sentimento_intensidade ELSE 0 END) as intensidade_media
		FROM historico_ligacoes
		WHERE idoso_id = $1
		  AND inicio_chamada > NOW() - INTERVAL '14 days'
	`

	var negativos, total int
	var intensidade float64

	err := pw.db.QueryRowContext(ctx, query, idosoID).Scan(&negativos, &total, &intensidade)
	if err != nil {
		return nil, err
	}

	if total < 5 {
		return nil, nil // Dados insuficientes
	}

	// Calcular probabilidade
	percentualNegativo := float64(negativos) / float64(total)
	probabilidade := (percentualNegativo * 0.6) + (intensidade / 10.0 * 0.4)

	// Determinar nível de risco
	var nivelRisco string
	switch {
	case probabilidade >= 0.75:
		nivelRisco = "critico"
	case probabilidade >= 0.50:
		nivelRisco = "alto"
	case probabilidade >= 0.30:
		nivelRisco = "medio"
	default:
		nivelRisco = "baixo"
	}

	// Só salvar se risco for médio ou superior
	if probabilidade >= 0.30 {
		prediction := &EmergencyPrediction{
			IdosoID:        idosoID,
			TipoEmergencia: "depressao_severa",
			Probabilidade:  probabilidade,
			NivelRisco:     nivelRisco,
			FatoresContribuintes: []string{
				fmt.Sprintf("%.0f%% de sentimentos negativos nos últimos 14 dias", percentualNegativo*100),
				fmt.Sprintf("Intensidade média de tristeza: %.1f/10", intensidade),
				fmt.Sprintf("Total de %d ligações analisadas", total),
			},
			SinaisDetectados: map[string]interface{}{
				"sentimentos_negativos": negativos,
				"total_ligacoes":        total,
				"percentual_negativo":   percentualNegativo,
				"intensidade_media":     intensidade,
			},
			Recomendacoes: []string{
				"Agendar consulta com psicólogo ou psiquiatra",
				"Aumentar frequência de ligações e monitoramento",
				"Notificar familiares sobre mudança de humor",
				"Avaliar necessidade de suporte emocional adicional",
			},
		}

		return prediction, nil
	}

	return nil, nil
}

// predictConfusion prediz risco de confusão mental
func (pw *PredictionWorker) predictConfusion(ctx context.Context, idosoID int) (*EmergencyPrediction, error) {
	query := `
		SELECT 
			COUNT(CASE WHEN sentimento_geral = 'confuso' THEN 1 END) as episodios_confusao,
			COUNT(*) as total_ligacoes,
			AVG(CASE WHEN sentimento_geral = 'confuso' THEN sentimento_intensidade ELSE 0 END) as intensidade_media
		FROM historico_ligacoes
		WHERE idoso_id = $1
		  AND inicio_chamada > NOW() - INTERVAL '7 days'
	`

	var confusao, total int
	var intensidade float64

	err := pw.db.QueryRowContext(ctx, query, idosoID).Scan(&confusao, &total, &intensidade)
	if err != nil {
		return nil, err
	}

	if total < 3 {
		return nil, nil
	}

	percentualConfusao := float64(confusao) / float64(total)
	probabilidade := (percentualConfusao * 0.7) + (intensidade / 10.0 * 0.3)

	var nivelRisco string
	switch {
	case probabilidade >= 0.60:
		nivelRisco = "critico"
	case probabilidade >= 0.40:
		nivelRisco = "alto"
	case probabilidade >= 0.20:
		nivelRisco = "medio"
	default:
		nivelRisco = "baixo"
	}

	if probabilidade >= 0.20 {
		prediction := &EmergencyPrediction{
			IdosoID:        idosoID,
			TipoEmergencia: "confusao_mental",
			Probabilidade:  probabilidade,
			NivelRisco:     nivelRisco,
			FatoresContribuintes: []string{
				fmt.Sprintf("%d episódio(s) de confusão em 7 dias", confusao),
				fmt.Sprintf("%.0f%% das ligações com sinais de confusão", percentualConfusao*100),
			},
			SinaisDetectados: map[string]interface{}{
				"episodios_confusao": confusao,
				"total_ligacoes":     total,
				"percentual":         percentualConfusao,
			},
			Recomendacoes: []string{
				"Avaliação médica urgente para descartar causas reversíveis",
				"Verificar medicações que podem causar confusão",
				"Aumentar supervisão e monitoramento",
				"Considerar avaliação neurológica",
			},
		}

		return prediction, nil
	}

	return nil, nil
}

// predictFallRisk prediz risco de queda baseado em mobilidade
func (pw *PredictionWorker) predictFallRisk(ctx context.Context, idosoID int) (*EmergencyPrediction, error) {
	// Buscar informações de mobilidade e histórico
	query := `
		SELECT 
			i.mobilidade,
			i.limitacoes_visuais,
			i.limitacoes_auditivas,
			COUNT(a.id) as total_alertas_queda
		FROM idosos i
		LEFT JOIN alertas a ON a.idoso_id = i.id 
			AND a.tipo = 'queda' 
			AND a.criado_em > NOW() - INTERVAL '90 days'
		WHERE i.id = $1
		GROUP BY i.mobilidade, i.limitacoes_visuais, i.limitacoes_auditivas
	`

	var mobilidade string
	var limitacoesVisuais, limitacoesAuditivas bool
	var totalAlertasQueda int

	err := pw.db.QueryRowContext(ctx, query, idosoID).Scan(
		&mobilidade, &limitacoesVisuais, &limitacoesAuditivas, &totalAlertasQueda,
	)
	if err != nil {
		return nil, err
	}

	// Calcular probabilidade baseada em fatores de risco
	probabilidade := 0.0
	fatores := []string{}

	// Mobilidade
	switch mobilidade {
	case "acamado":
		probabilidade += 0.10
		fatores = append(fatores, "Mobilidade: acamado (risco ao transferir)")
	case "cadeira_rodas":
		probabilidade += 0.20
		fatores = append(fatores, "Mobilidade: cadeira de rodas")
	case "auxiliado":
		probabilidade += 0.35
		fatores = append(fatores, "Mobilidade: necessita auxílio")
	case "independente":
		probabilidade += 0.05
	}

	// Limitações sensoriais
	if limitacoesVisuais {
		probabilidade += 0.25
		fatores = append(fatores, "Limitações visuais")
	}
	if limitacoesAuditivas {
		probabilidade += 0.10
		fatores = append(fatores, "Limitações auditivas")
	}

	// Histórico de quedas
	if totalAlertasQueda > 0 {
		probabilidade += float64(totalAlertasQueda) * 0.15
		fatores = append(fatores, fmt.Sprintf("%d queda(s) nos últimos 90 dias", totalAlertasQueda))
	}

	// Limitar probabilidade a 1.0
	if probabilidade > 1.0 {
		probabilidade = 1.0
	}

	var nivelRisco string
	switch {
	case probabilidade >= 0.70:
		nivelRisco = "critico"
	case probabilidade >= 0.50:
		nivelRisco = "alto"
	case probabilidade >= 0.30:
		nivelRisco = "medio"
	default:
		nivelRisco = "baixo"
	}

	if probabilidade >= 0.30 {
		prediction := &EmergencyPrediction{
			IdosoID:              idosoID,
			TipoEmergencia:       "queda",
			Probabilidade:        probabilidade,
			NivelRisco:           nivelRisco,
			FatoresContribuintes: fatores,
			SinaisDetectados: map[string]interface{}{
				"mobilidade":           mobilidade,
				"limitacoes_visuais":   limitacoesVisuais,
				"limitacoes_auditivas": limitacoesAuditivas,
				"historico_quedas":     totalAlertasQueda,
			},
			Recomendacoes: []string{
				"Avaliar ambiente doméstico para riscos de queda",
				"Considerar uso de dispositivos de auxílio (andador, bengala)",
				"Fisioterapia para fortalecimento e equilíbrio",
				"Instalar barras de apoio em banheiro e corredores",
			},
		}

		return prediction, nil
	}

	return nil, nil
}

// savePrediction salva predição no banco
func (pw *PredictionWorker) savePrediction(ctx context.Context, pred *EmergencyPrediction) error {
	fatoresJSON, _ := json.Marshal(pred.FatoresContribuintes)
	sinaisJSON, _ := json.Marshal(pred.SinaisDetectados)
	recomendacoesJSON, _ := json.Marshal(pred.Recomendacoes)

	query := `
		INSERT INTO predicoes_emergencia (
			idoso_id, tipo_emergencia, probabilidade, nivel_risco,
			fatores_contribuintes, sinais_detectados, recomendacoes,
			validade_ate
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW() + INTERVAL '7 days')
		ON CONFLICT DO NOTHING
	`

	_, err := pw.db.ExecContext(ctx, query,
		pred.IdosoID,
		pred.TipoEmergencia,
		pred.Probabilidade,
		pred.NivelRisco,
		fatoresJSON,
		sinaisJSON,
		recomendacoesJSON,
	)

	if err == nil {
		log.Printf("⚠️ Predição '%s' salva para idoso %d (risco: %s, prob: %.0f%%)",
			pred.TipoEmergencia, pred.IdosoID, pred.NivelRisco, pred.Probabilidade*100)
	}

	return err
}
