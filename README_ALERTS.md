# EVA-Mind - Sistema de Alertas Refatorado

## Melhorias Implementadas

### 1. Sistema de Alertas Multi-Canal
- ✅ Push Notifications via Firebase (implementado)
- ⏳ SMS via Twilio (estrutura preparada)
- ⏳ Email (estrutura preparada)
- ⏳ Ligação telefônica (estrutura preparada)

### 2. Gestão de Alertas
- **Confirmação de Leitura**: Tracking de quando alertas são visualizados
- **Escalamento Automático**: Alertas críticos não visualizados são escalonados
- **Múltiplos Contatos**: Suporte para vários cuidadores com prioridades
- **Validação de Tokens**: Verifica se tokens FCM são válidos antes de enviar
- **Histórico de Alertas**: Auditoria completa de todas as ações

### 3. Sistema de Fallback
```
Push Notification → SMS → Email → Ligação Telefônica
```

## Instalação

### 1. Aplicar Migrações no Banco de Dados
```bash
psql -h 34.89.62.186 -U your_user -d eva_db -f migrations.sql
```

### 2. Configurar Variáveis de Ambiente
Copie o arquivo `.env` e configure:

```env
# Obrigatórias
DATABASE_URL=postgresql://...
GOOGLE_API_KEY=your_key
FIREBASE_CREDENTIALS_PATH=/path/to/credentials.json

# Opcionais (para fallback)
TWILIO_ACCOUNT_SID=your_sid
TWILIO_AUTH_TOKEN=your_token
ENABLE_SMS_FALLBACK=true
```

### 3. Substituir Arquivos no Projeto
```bash
# Copiar arquivos refatorados
cp firebase.go internal/push/
cp tools.go internal/gemini/
cp scheduler.go internal/scheduler/
cp config.go internal/config/
```

## Estrutura de Banco de Dados

### Tabela `alertas` (atualizada)
```sql
- visualizado (boolean)
- data_visualizacao (timestamp)
- necessita_escalamento (boolean)
- tempo_escalamento (timestamp)
- tentativas_envio (integer)
```

### Tabela `contatos_emergencia` (nova)
```sql
- idoso_id (FK)
- nome, telefone, email
- prioridade (1, 2, 3...)
- metodo_preferido ('push', 'sms', 'email', 'call')
```

### Tabela `historico_alertas` (nova)
```sql
- alerta_id (FK)
- acao ('enviado', 'visualizado', 'escalado', 'falha')
- metodo, detalhes, sucesso
```

## API Endpoints para o App Android

### Confirmar Visualização de Alerta
```http
POST /api/alerts/:id/acknowledge
Authorization: Bearer <token>

{
  "visualizado_em": "2025-12-29T10:30:00Z",
  "cuidador_id": 123
}
```

### Listar Alertas Pendentes
```http
GET /api/alerts/pending
Authorization: Bearer <token>

Response:
{
  "alertas": [
    {
      "id": 456,
      "mensagem": "Maria precisa de ajuda: dor no peito",
      "severidade": "critica",
      "criado_em": "2025-12-29T10:25:00Z",
      "visualizado": false
    }
  ]
}
```

## Fluxo de Alertas

### Alerta Crítico
```
1. IA detecta emergência → tools.AlertFamily()
2. Busca todos os cuidadores ativos
3. Tenta enviar Push Notification
4. Se falhar → marca para escalamento (5 min)
5. Scheduler verifica alertas não visualizados
6. Se tempo expirou → tenta SMS/Email/Call
```

### Chamada Não Atendida
```
1. Push enviado → status 'em_andamento'
2. 45 segundos sem resposta → scheduler.checkMissedCalls()
3. Status → 'nao_atendido'
4. Registra em: historico_ligacoes, alertas, timeline
5. Notifica cuidador com severidade 'aviso'
```

## Testes

### Teste de Envio de Alerta
```go
// Em tools_test.go
func TestAlertFamily(t *testing.T) {
    db := setupTestDB()
    pushService := setupTestPush()
    
    err := AlertFamily(db, pushService, 1, "teste de alerta")
    assert.NoError(t, err)
}
```

### Teste de Escalamento
```go
// Em scheduler_test.go
func TestCheckUnacknowledgedAlerts(t *testing.T) {
    // Criar alerta crítico com tempo_escalamento expirado
    // Verificar se checkUnacknowledgedAlerts() o processa
}
```

## Próximos Passos (TODOs)

### 1. Implementar SMS via Twilio
```go
// Em tools.go, na função AlertFamilyWithSeverity
func sendSMSFallback(phone, message string) error {
    client := twilio.NewClient(cfg.TwilioAccountSID, cfg.TwilioAuthToken)
    params := &openapi.CreateMessageParams{}
    params.SetTo(phone)
    params.SetFrom(cfg.TwilioPhoneNumber)
    params.SetBody(message)
    
    resp, err := client.Api.CreateMessage(params)
    return err
}
```

### 2. Implementar Email via SMTP
```go
func sendEmailFallback(email, subject, body string) error {
    m := gomail.NewMessage()
    m.SetHeader("From", "eva@yourdomain.com")
    m.SetHeader("To", email)
    m.SetHeader("Subject", subject)
    m.SetBody("text/html", body)
    
    d := gomail.NewDialer("smtp.gmail.com", 587, "user", "pass")
    return d.DialAndSend(m)
}
```

### 3. Implementar Ligação Telefônica
```go
func makeEmergencyCall(phone, message string) error {
    // Usar Twilio Voice API com TwiML
    // Tocar mensagem gravada ou text-to-speech
}
```

### 4. Criar Endpoints REST
```go
// Em main.go ou routes.go
router.POST("/api/alerts/:id/acknowledge", acknowledgeAlert)
router.GET("/api/alerts/pending", getPendingAlerts)
router.GET("/api/alerts/history/:idoso_id", getAlertHistory)
```

## Monitorização

### Logs Importantes
```
✅ Alert sent to 2 of 2 caregivers
⚠️ Nenhum push notification enviado, tentando fallbacks
🚨 ESCALANDO alerta não visualizado - ID: 456
📵 Cuidador notificado sobre chamada perdida
```

### Métricas a Monitorizar
- Taxa de entrega de alertas (push/total)
- Tempo médio até visualização
- Taxa de escalamento
- Chamadas não atendidas por dia

## Segurança

### Validação de Tokens
```go
// Antes de enviar, sempre validar
if !pushService.ValidateToken(deviceToken) {
    // Marcar token como inválido no banco
    // Solicitar novo token ao app
}
```

### Rate Limiting
- Limitar tentativas de envio de alertas (5 por minuto)
- Prevenir spam de notificações
- Implementar exponential backoff

## Suporte

Para questões sobre a implementação:
1. Verifique os logs: `journalctl -u eva-mind -f`
2. Consulte o histórico de alertas no banco
3. Valide tokens FCM com Firebase Console

---

**Versão**: 2.0.0  
**Data**: 29 de Dezembro de 2025  
**Autor**: Claude (Anthropic)
