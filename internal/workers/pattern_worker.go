package workers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// PatternWorker detecta padrões comportamentais
type PatternWorker struct {
	db *sql.DB
}

// NewPatternWorker cria um novo worker de padrões
func NewPatternWorker(db *sql.DB) *PatternWorker {
	return &PatternWorker{db: db}
}

// Name retorna o nome do worker
func (pw *PatternWorker) Name() string {
	return "Pattern Detector"
}

// Interval retorna o intervalo de execução (6 horas)
func (pw *PatternWorker) Interval() time.Duration {
	return 6 * time.Hour
}

// Run executa a detecção de padrões
func (pw *PatternWorker) Run(ctx context.Context) error {
	log.Println("🔍 Iniciando detecção de padrões comportamentais...")

	// Buscar todos os idosos ativos
	idosos, err := pw.getActiveIdosos(ctx)
	if err != nil {
		return fmt.Errorf("erro ao buscar idosos: %w", err)
	}

	log.Printf("📊 Analisando padrões para %d idoso(s)...", len(idosos))

	totalPadroes := 0
	for _, idosoID := range idosos {
		// Detectar padrões de sono
		if pattern, err := pw.detectSleepPattern(ctx, idosoID); err == nil && pattern != nil {
			if err := pw.savePattern(ctx, pattern); err == nil {
				totalPadroes++
			}
		}

		// Detectar padrões de humor
		if pattern, err := pw.detectMoodPattern(ctx, idosoID); err == nil && pattern != nil {
			if err := pw.savePattern(ctx, pattern); err == nil {
				totalPadroes++
			}
		}

		// Detectar padrões de medicação
		if pattern, err := pw.detectMedicationPattern(ctx, idosoID); err == nil && pattern != nil {
			if err := pw.savePattern(ctx, pattern); err == nil {
				totalPadroes++
			}
		}
	}

	log.Printf("✅ Detecção concluída: %d padrão(ões) detectado(s)", totalPadroes)
	return nil
}

// getActiveIdosos retorna lista de idosos ativos
func (pw *PatternWorker) getActiveIdosos(ctx context.Context) ([]int, error) {
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

// BehaviorPattern representa um padrão comportamental
type BehaviorPattern struct {
	IdosoID           int
	TipoPadrao        string
	Descricao         string
	Frequencia        string
	Confianca         float64
	DadosEstatisticos map[string]interface{}
}

// detectSleepPattern detecta padrões de sono
func (pw *PatternWorker) detectSleepPattern(ctx context.Context, idosoID int) (*BehaviorPattern, error) {
	query := `
		SELECT 
			EXTRACT(HOUR FROM inicio_chamada) as hora,
			COUNT(*) as total
		FROM historico_ligacoes
		WHERE idoso_id = $1
		  AND inicio_chamada > NOW() - INTERVAL '30 days'
		  AND tarefa_concluida = true
		GROUP BY EXTRACT(HOUR FROM inicio_chamada)
		ORDER BY hora
	`

	rows, err := pw.db.QueryContext(ctx, query, idosoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	horariosAtivos := make(map[int]int)
	totalLigacoes := 0

	for rows.Next() {
		var hora, total int
		if err := rows.Scan(&hora, &total); err != nil {
			continue
		}
		horariosAtivos[hora] = total
		totalLigacoes += total
	}

	if totalLigacoes < 10 {
		return nil, nil // Dados insuficientes
	}

	// Detectar horários de baixa atividade (sono)
	horasSono := []int{}
	mediaAtividade := float64(totalLigacoes) / 24.0

	for hora := 0; hora < 24; hora++ {
		if float64(horariosAtivos[hora]) < mediaAtividade*0.3 {
			horasSono = append(horasSono, hora)
		}
	}

	if len(horasSono) >= 6 {
		// Encontrar intervalo contínuo de sono
		inicio, fim := pw.findContinuousInterval(horasSono)

		pattern := &BehaviorPattern{
			IdosoID:    idosoID,
			TipoPadrao: "horario_sono",
			Descricao:  fmt.Sprintf("Padrão de sono detectado: %02d:00 - %02d:00", inicio, fim),
			Frequencia: "diario",
			Confianca:  0.85,
			DadosEstatisticos: map[string]interface{}{
				"hora_inicio":           inicio,
				"hora_fim":              fim,
				"horas_sono":            len(horasSono),
				"total_dias_analisados": 30,
				"total_ligacoes":        totalLigacoes,
			},
		}

		return pattern, nil
	}

	return nil, nil
}

// detectMoodPattern detecta padrões de humor
func (pw *PatternWorker) detectMoodPattern(ctx context.Context, idosoID int) (*BehaviorPattern, error) {
	query := `
		SELECT 
			sentimento_geral,
			COUNT(*) as total,
			AVG(sentimento_intensidade) as intensidade_media
		FROM historico_ligacoes
		WHERE idoso_id = $1
		  AND inicio_chamada > NOW() - INTERVAL '30 days'
		  AND sentimento_geral IS NOT NULL
		GROUP BY sentimento_geral
		ORDER BY total DESC
		LIMIT 1
	`

	var sentimento string
	var total int
	var intensidadeMedia float64

	err := pw.db.QueryRowContext(ctx, query, idosoID).Scan(&sentimento, &total, &intensidadeMedia)
	if err != nil {
		return nil, err
	}

	if total >= 10 {
		confianca := float64(total) / 30.0
		if confianca > 1.0 {
			confianca = 1.0
		}

		pattern := &BehaviorPattern{
			IdosoID:    idosoID,
			TipoPadrao: "humor_recorrente",
			Descricao:  fmt.Sprintf("Humor predominante: %s (%d ocorrências em 30 dias)", sentimento, total),
			Frequencia: "semanal",
			Confianca:  confianca,
			DadosEstatisticos: map[string]interface{}{
				"sentimento_predominante": sentimento,
				"ocorrencias":             total,
				"intensidade_media":       intensidadeMedia,
				"dias_analisados":         30,
			},
		}

		return pattern, nil
	}

	return nil, nil
}

// detectMedicationPattern detecta padrões de adesão à medicação
func (pw *PatternWorker) detectMedicationPattern(ctx context.Context, idosoID int) (*BehaviorPattern, error) {
	query := `
		SELECT 
			COUNT(*) as total_agendamentos,
			COUNT(CASE WHEN medicamento_tomado = true THEN 1 END) as medicamentos_tomados
		FROM agendamentos
		WHERE idoso_id = $1
		  AND tipo = 'lembrete_medicamento'
		  AND data_hora_agendada > NOW() - INTERVAL '30 days'
	`

	var totalAgendamentos, medicamentosTomados int

	err := pw.db.QueryRowContext(ctx, query, idosoID).Scan(&totalAgendamentos, &medicamentosTomados)
	if err != nil {
		return nil, err
	}

	if totalAgendamentos >= 10 {
		taxaAdesao := float64(medicamentosTomados) / float64(totalAgendamentos)

		var descricao string
		if taxaAdesao >= 0.9 {
			descricao = fmt.Sprintf("Excelente adesão à medicação: %.0f%%", taxaAdesao*100)
		} else if taxaAdesao >= 0.7 {
			descricao = fmt.Sprintf("Boa adesão à medicação: %.0f%%", taxaAdesao*100)
		} else if taxaAdesao >= 0.5 {
			descricao = fmt.Sprintf("Adesão moderada à medicação: %.0f%%", taxaAdesao*100)
		} else {
			descricao = fmt.Sprintf("Baixa adesão à medicação: %.0f%% - ATENÇÃO", taxaAdesao*100)
		}

		pattern := &BehaviorPattern{
			IdosoID:    idosoID,
			TipoPadrao: "medicacao_adesao",
			Descricao:  descricao,
			Frequencia: "diario",
			Confianca:  0.90,
			DadosEstatisticos: map[string]interface{}{
				"total_agendamentos":   totalAgendamentos,
				"medicamentos_tomados": medicamentosTomados,
				"taxa_adesao":          taxaAdesao,
				"dias_analisados":      30,
			},
		}

		return pattern, nil
	}

	return nil, nil
}

// savePattern salva padrão no banco
func (pw *PatternWorker) savePattern(ctx context.Context, pattern *BehaviorPattern) error {
	dadosJSON, _ := json.Marshal(pattern.DadosEstatisticos)

	query := `
		INSERT INTO padroes_comportamento (
			idoso_id, tipo_padrao, descricao, frequencia, 
			confianca, dados_estatisticos
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (idoso_id, tipo_padrao) 
		DO UPDATE SET 
			descricao = EXCLUDED.descricao,
			confianca = EXCLUDED.confianca,
			dados_estatisticos = EXCLUDED.dados_estatisticos,
			ultima_confirmacao = NOW(),
			ocorrencias = padroes_comportamento.ocorrencias + 1,
			atualizado_em = NOW()
	`

	_, err := pw.db.ExecContext(ctx, query,
		pattern.IdosoID,
		pattern.TipoPadrao,
		pattern.Descricao,
		pattern.Frequencia,
		pattern.Confianca,
		dadosJSON,
	)

	if err == nil {
		log.Printf("✅ Padrão '%s' salvo para idoso %d", pattern.TipoPadrao, pattern.IdosoID)
	}

	return err
}

// findContinuousInterval encontra o maior intervalo contínuo em uma lista de horas
func (pw *PatternWorker) findContinuousInterval(horas []int) (int, int) {
	if len(horas) == 0 {
		return 0, 0
	}

	inicio := horas[0]
	fim := horas[0]

	for i := 1; i < len(horas); i++ {
		if horas[i] == horas[i-1]+1 || (horas[i-1] == 23 && horas[i] == 0) {
			fim = horas[i]
		}
	}

	return inicio, fim
}
