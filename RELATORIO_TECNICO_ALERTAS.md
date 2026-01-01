# Relatório Técnico: Sistema de Alertas para Família

Analisando o código fornecido, aqui está um detalhamento completo sobre o sistema de alertas implementado no EVA-Mind:

---

## 1. Tipos de Alerta Implementados

### ❌ **Email**
**NÃO IMPLEMENTADO**. O sistema não possui configuração SMTP, integração com SendGrid ou qualquer outro serviço de envio de emails.

### ✅ **Push Notifications** (Método Principal)
**TOTALMENTE IMPLEMENTADO** via Firebase Cloud Messaging (FCM). Existem 3 tipos de notificações push:

#### **A. Alerta de Emergência** (`alert_family`)
- **Gatilho**: Detectado pela IA Gemini durante conversa com o idoso
- **Arquivo**: `tools.go` → função `AlertFamily()`
- **Exemplos de ativação**:
  - Relato de dor no peito
  - Confusão mental súbita
  - Queda
  - Sinais de AVC/infarto

```go
// tools.go - linha ~75
message := &messaging.Message{
    Token: token,
    Notification: &messaging.Notification{
        Title: "⚠️ Alerta EVA",
        Body:  fmt.Sprintf("%s precisa de atenção: %s", elderName, reason),
    },
    Android: &messaging.AndroidConfig{
        Priority: "high",
        Notification: &messaging.AndroidNotification{
            Sound:    "alert",
            Priority: messaging.PriorityHigh,
            Color:    "#FF0000", // Vermelho crítico
        },
    },
}
```

#### **B. Confirmação de Medicamento**
- **Gatilho**: Idoso confirma que tomou o remédio
- **Arquivo**: `tools.go` → função `ConfirmMedication()`
- **Tipo**: Notificação informativa (prioridade normal)

#### **C. Chamada Não Atendida**
- **Gatilho**: Idoso não atende push notification em 45 segundos
- **Arquivo**: `scheduler.go` → função `checkMissedCalls()`
- **Comportamento**:

```go
// scheduler.go - linha ~88
WHERE a.status = 'em_andamento' 
  AND a.data_hora_agendada < (NOW() - INTERVAL '45 seconds')
```

### ❌ **Ligação Telefônica**
**NÃO IMPORTANTE** no fluxo de alertas. Embora existam credenciais Twilio no `config.go`:

```go
// config.go
TwilioAccountSID  string
TwilioAuthToken   string
TwilioPhoneNumber string
```

**Estas credenciais NÃO são utilizadas** em `tools.go`, `firebase.go` ou `scheduler.go` para realizar chamadas à família. O Twilio está configurado apenas para receber chamadas do idoso via `websocket.go`.

---

## 2. Fluxo Completo de Notificação (Passo a Passo)

### 🔴 **Cenário 1: Emergência Detectada pela IA**

```
┌─────────────────┐
│   websocket.go  │  1️⃣ Idoso fala "Estou com dor no peito"
│  (Conversa Ativa)│     Gemini detecta emergência
└────────┬────────┘
         │ executeTool(session, fnCall)
         ▼
┌─────────────────┐
│    tools.go     │  2️⃣ AlertFamily(db, push, idosoID, "dor no peito")
│  AlertFamily()  │     SELECT device_token FROM cuidadores WHERE...
└────────┬────────┘
         │ pushService.SendAlertNotification()
         ▼
┌─────────────────┐
│   firebase.go   │  3️⃣ Envia via FCM com prioridade ALTA
│ SendAlertNoti() │     Message com som "alert", cor vermelha
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  📱 CELULAR DO  │  4️⃣ Notificação aparece MESMO com app fechado
│    CUIDADOR     │     "⚠️ Maria precisa de atenção: dor no peito"
└─────────────────┘
```

**Evidências no Código:**

```go
// websocket.go - linha ~203
if fnCall, ok := partMap["functionCall"].(map[string]interface{}); ok {
    s.executeTool(session, fnCall)
}

// websocket.go - linha ~213
case "alert_family":
    reason, _ := args["reason"].(string)
    if err := gemini.AlertFamily(s.db, s.pushService, session.IdosoID, reason); err != nil {
        log.Printf("❌ Erro ao enviar alerta")
    }
```

### 🟡 **Cenário 2: Chamada Não Atendida**

```
┌─────────────────┐
│  scheduler.go   │  1️⃣ Verifica a cada 30 segundos
│   (Watchdog)    │     checkMissedCalls()
└────────┬────────┘
         │ Query: status='em_andamento' AND +45 sec
         ▼
┌─────────────────┐
│  BANCO DE DADOS │  2️⃣ 4 operações no PostgreSQL:
│   PostgreSQL    │     - UPDATE agendamentos → 'nao_atendido'
└────────┬────────┘     - INSERT historico_ligacoes
         │              - INSERT alertas
         │              - INSERT timeline
         ▼
┌─────────────────┐
│   firebase.go   │  3️⃣ SendMissedCallAlert(token, nome)
│ SendMissedCall()│     Notificação com urgência ALTA
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  📱 CELULAR DO  │  4️⃣ "⚠️ Maria não atendeu a chamada da EVA"
│    CUIDADOR     │
└─────────────────┘
```

**Evidências no Código:**

```go
// scheduler.go - linha ~138
_, errAlerta := s.db.Exec(`
    INSERT INTO alertas (
        idoso_id, ligacao_id, tipo, severidade, mensagem,
        destinatarios, enviado, data_envio, criado_em
    ) VALUES ($1, $2, 'nao_atende_telefone', 'aviso', $3, $4, true, NOW(), NOW())
`, idosoID, historicoID, mensagem, `["cuidador"]`)

// scheduler.go - linha ~164
if tokenCuidador.Valid {
    errPush := s.pushService.SendMissedCallAlert(tokenCuidador.String, nomeIdoso)
}
```

---

## 3. Análise de Risco de Falhas

### ⚠️ **Pontos Críticos Identificados**

#### **A. Dependência Total do Firebase**
```go
// Se o Firebase estiver offline ou token inválido, NENHUM alerta chega
if deviceToken == "" {
    return fmt.Errorf("device token is empty") // Falha silenciosa
}
```

**Problema**: Não existe fallback para SMS ou email.

#### **B. Múltiplos Single Points of Failure**

1. **Token Desatualizado**: Se o app for desinstalado, o `device_token` no banco fica inválido mas o sistema não sabe.
   
2. **App em Background**: Android pode matar o processo do app. Embora FCM use prioridade alta:

```go
Android: &messaging.AndroidConfig{
    Priority: "high", // Força acordar, mas não garante 100%
}
```

3. **Celular Sem Internet**: Se o cuidador estiver offline, a notificação fica na fila do Firebase (mas pode expirar).

#### **C. Falta de Confirmação de Recebimento**

```go
// firebase.go - linha ~80
response, err := s.client.Send(s.ctx, message)
if err != nil {
    return fmt.Errorf("error sending call push: %w", err)
}
log.Printf("🚀 Ligação iniciada... : %s", response)
// ❌ NÃO verifica se o cuidador VIU a notificação
```

---

## 4. Recomendações de Melhoria

### 🔧 **Implementações Sugeridas**

#### **1. Sistema de Fallback em Cascata**
```go
func AlertFamilyWithFallback(db, push, sms, email, idosoID, reason) error {
    // Tentativa 1: Push (rápido)
    if err := push.Send(); err == nil {
        return nil
    }
    
    // Tentativa 2: SMS via Twilio (30 seg depois)
    time.Sleep(30 * time.Second)
    if err := sms.Send(); err == nil {
        return nil
    }
    
    // Tentativa 3: Email (1 min depois)
    time.Sleep(1 * time.Minute)
    return email.Send()
}
```

#### **2. Confirmação de Leitura**
```go
// Endpoint REST que o app Android chama ao abrir a notificação
POST /api/alerts/:id/acknowledge
{
    "alerta_id": 123,
    "visualizado_em": "2025-12-28T10:30:00Z"
}
```

#### **3. Escalação Automática**
```go
// scheduler.go - nova função
func (s *Scheduler) escalateUnacknowledgedAlerts() {
    // Se alerta crítico não foi visto em 5 min → Liga para telefone fixo
    query := `
        SELECT a.id, i.telefone_fixo
        FROM alertas a
        JOIN idosos i ON i.id = a.idoso_id
        WHERE a.severidade = 'critica'
          AND a.visualizado = false
          AND a.criado_em < NOW() - INTERVAL '5 minutes'
    `
    // Implementar ligação via Twilio Voice API
}
```

#### **4. Múltiplos Contatos**
```sql
-- Criar tabela de contatos de emergência
CREATE TABLE contatos_emergencia (
    id SERIAL PRIMARY KEY,
    idoso_id INT REFERENCES idosos(id),
    nome VARCHAR(255),
    telefone VARCHAR(20),
    email VARCHAR(255),
    device_token TEXT,
    prioridade INT, -- 1=primário, 2=secundário, 3=terciário
    metodo_preferido VARCHAR(20) -- 'push', 'sms', 'call', 'email'
);
```

---

## 5. Resumo Executivo

| Funcionalidade | Status | Observações |
|---|---|---|
| **Push Notifications** | ✅ Implementado | Via Firebase FCM, 3 tipos de alertas |
| **Email** | ❌ Não implementado | Nenhuma configuração SMTP |
| **Ligação Telefônica** | ⚠️ Parcialmente configurado | Credenciais Twilio existem mas não são usadas no alerta |
| **SMS** | ❌ Não implementado | Twilio configurado mas não utilizado |
| **Fallback** | ❌ Não existe | Se push falhar, não há alternativa |
| **Confirmação de Leitura** | ❌ Não implementado | Sistema não sabe se cuidador viu alerta |

### **Arquivos Envolvidos (por prioridade)**
1. **`firebase.go`** - Motor principal de notificações
2. **`tools.go`** - Lógica de alertas acionados pela IA
3. **`scheduler.go`** - Monitor de chamadas perdidas
4. **`websocket.go`** - Conversa ativa que aciona emergências
5. **`config.go`** - Credenciais (Firebase obrigatório)

### **Conclusão**
O sistema atual é **funcional mas frágil**. Depende 100% do Firebase e não possui redundância. Para ambientes de produção com idosos em risco, recomenda-se implementar os fallbacks sugeridos na seção 4.
