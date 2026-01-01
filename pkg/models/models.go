package models

import "time"

type Agendamento struct {
	ID                   int64      `json:"id"`
	IdosoID              int64      `json:"idoso_id"`
	Tipo                 string     `json:"tipo"`
	DataHoraAgendada     time.Time  `json:"data_hora_agendada"`
	DataHoraRealizada    *time.Time `json:"data_hora_realizada,omitempty"`
	Status               string     `json:"status"`
	Prioridade           string     `json:"prioridade"`
	DadosTarefa          string     `json:"dados_tarefa"` // JSON string
	MaxRetries           int        `json:"max_retries"`
	TentativasRealizadas int        `json:"tentativas_realizadas"`
}

type Idoso struct {
	ID                  int64     `json:"id"`
	Nome                string    `json:"nome"`
	DataNascimento      time.Time `json:"data_nascimento"`
	Telefone            string    `json:"telefone"`
	CPF                 string    `json:"cpf"`
	DeviceToken         string    `json:"device_token"`
	Ativo               bool      `json:"ativo"`
	NivelCognitivo      string    `json:"nivel_cognitivo"`
	LimitacoesAuditivas bool      `json:"limitacoes_auditivas"`
	UsaAparelhoAuditivo bool      `json:"usa_aparelho_auditivo"`
	TomVoz              string    `json:"tom_voz"`
	PreferenciaHorario  string    `json:"preferencia_horario_ligacao"`
}
