package database

import (
	"database/sql"
	"fmt"
	"time"
)

type Agendamento struct {
	ID                   int64
	IdosoID              int64
	Tipo                 string
	DataHoraAgendada     time.Time
	DataHoraRealizada    *time.Time
	Status               string
	Prioridade           string
	DadosTarefa          string
	MaxRetries           int
	TentativasRealizadas int
}

type Idoso struct {
	ID                  int64
	Nome                string
	DataNascimento      time.Time
	Telefone            string
	CPF                 string
	DeviceToken         string
	Ativo               bool
	NivelCognitivo      string
	LimitacoesAuditivas bool
	UsaAparelhoAuditivo bool
	TomVoz              string
	PreferenciaHorario  string
}

func (db *DB) GetPendingAgendamentos(limit int) ([]Agendamento, error) {
	query := `
		SELECT id, idoso_id, tipo, data_hora_agendada, data_hora_realizada, status, prioridade, dados_tarefa, max_retries, tentativas_realizadas
		FROM agendamentos
		WHERE status = 'agendado'
		  AND data_hora_agendada <= $1
		ORDER BY data_hora_agendada ASC
		LIMIT $2
	`

	rows, err := db.conn.Query(query, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query agendamentos: %w", err)
	}
	defer rows.Close()

	var agendamentos []Agendamento
	for rows.Next() {
		var a Agendamento
		err := rows.Scan(
			&a.ID, &a.IdosoID, &a.Tipo, &a.DataHoraAgendada, &a.DataHoraRealizada,
			&a.Status, &a.Prioridade, &a.DadosTarefa, &a.MaxRetries, &a.TentativasRealizadas,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan: %w", err)
		}
		agendamentos = append(agendamentos, a)
	}

	return agendamentos, nil
}

func (db *DB) GetIdoso(id int64) (*Idoso, error) {
	query := `
		SELECT 
			id, nome, data_nascimento, telefone, cpf, device_token, 
			ativo, nivel_cognitivo, limitacoes_auditivas, usa_aparelho_auditivo, 
			tom_voz, preferencia_horario_ligacao 
		FROM idosos 
		WHERE id = $1
	`

	var idoso Idoso
	err := db.conn.QueryRow(query, id).Scan(
		&idoso.ID, &idoso.Nome, &idoso.DataNascimento, &idoso.Telefone, &idoso.CPF, &idoso.DeviceToken,
		&idoso.Ativo, &idoso.NivelCognitivo, &idoso.LimitacoesAuditivas, &idoso.UsaAparelhoAuditivo,
		&idoso.TomVoz, &idoso.PreferenciaHorario,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("idoso not found")
		}
		return nil, fmt.Errorf("failed to query: %w", err)
	}

	return &idoso, nil
}

func (db *DB) UpdateAgendamentoStatus(id int64, status string) error {
	query := `UPDATE agendamentos SET status = $1, atualizado_em = CURRENT_TIMESTAMP WHERE id = $2`

	result, err := db.conn.Exec(query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("agendamento not found")
	}

	return nil
}

func (db *DB) GetIdosoByCPF(cpf string) (*Idoso, error) {
	// Esta query usa regexp_replace para ignorar pontos, traços e espaços
	// tanto no banco quanto no que o usuário digitou.
	query := `
		SELECT 
			id, nome, data_nascimento, telefone, cpf, device_token, 
			ativo, nivel_cognitivo, limitacoes_auditivas, usa_aparelho_auditivo, 
			tom_voz, preferencia_horario_ligacao 
		FROM idosos 
		WHERE regexp_replace(cpf, '\D', '', 'g') = regexp_replace($1, '\D', '', 'g')
	`

	var idoso Idoso
	err := db.conn.QueryRow(query, cpf).Scan(
		&idoso.ID, &idoso.Nome, &idoso.DataNascimento, &idoso.Telefone, &idoso.CPF, &idoso.DeviceToken,
		&idoso.Ativo, &idoso.NivelCognitivo, &idoso.LimitacoesAuditivas, &idoso.UsaAparelhoAuditivo,
		&idoso.TomVoz, &idoso.PreferenciaHorario,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("idoso não encontrado com CPF: %s", cpf)
		}
		return nil, fmt.Errorf("erro ao consultar CPF: %w", err)
	}

	return &idoso, nil
}
