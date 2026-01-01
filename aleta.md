# EVA-Mind - Sistema de Alertas para Família

## 📋 Visão Geral

Sistema completo de notificações para alertar familiares/cuidadores sobre a saúde e segurança de idosos.

## 🎯 Funcionalidades

### ✅ Implementado

1. **Push Notifications via Firebase**
   - Alerta de emergência (dor, confusão, queda)
   - Chamada não atendida
   - Confirmação de medicamento

2. **Monitoramento Automático**
   - Watchdog detecta chamadas perdidas (60s timeout)
   - Scheduler verifica agendamentos a cada 30s
   - Análise de conversas com IA

3. **Registro Completo**
   - Histórico de ligações
   - Timeline de eventos
   - Alertas categorizados

### ❌ Não Implementado

- Email
- SMS
- Ligação telefônica para família

---

## 🏗️ Arquitetura

```
┌─────────────────┐
│  Conversa Ativa │  → IA detecta emergência
│   (WebSocket)   │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│    tools.go     │  → AlertFamily()
│  (Lógica Core)  │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   firebase.go   │  → Envia push FCM
│  (Motor Alerts) │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  📱 Celular do  │  → Notificação aparece
│    Cuidador     │
└─────────────────┘
```

**Fluxo Paralelo (Watchdog):**

```
scheduler.go → Verifica chamadas > 60s → firebase.go → 📱 Alerta
```

---

## 📁 Estrutura de Arquivos

```
eva-mind/
├── .env                          # Configurações
├── serviceAccountKey.json        # Credenciais Firebase
├── main.go                       # Inicialização
│
├── internal/
│   ├── config/
│   │   └── config.go            # Carrega .env
│   │
│   ├── database/
│   │   └── db.go                # Conexão PostgreSQL
│   │
│   ├── push/
│   │   └── firebase.go          # ⚠️ CRÍTICO - Envia notificações
│   │
│   ├── gemini/
│   │   ├── tools.go             # AlertFamily(), ConfirmMedication()
│   │   ├── client.go            # WebSocket Gemini API
│   │   └── analysis.go          # Análise de conversas
│   │
│   ├── scheduler/
│   │   └── scheduler.go         # Watchdog de chamadas
│   │
│   └── signaling/
│       └── websocket.go         # Conversas em tempo real
```

---

## 🚀 Como Usar

### 1. Pré-requisitos

```bash
# Go 1.21+
go version

# PostgreSQL rodando em 34.89.62.186
psql -h 34.89.62.186 -U postgres -d eva
```

### 2. Configurar Firebase

1. Baixe `serviceAccountKey.json` do Firebase Console
2. Coloque na raiz do projeto
3. Verifique permissões:
   ```bash
   chmod 600 serviceAccountKey.json
   ```

### 3. Configurar `.env`

```bash
cp .env.example .env
nano .env
```

**Campos obrigatórios:**
- `DATABASE_URL`
- `GOOGLE_API_KEY`
- `FIREBASE_CREDENTIALS_PATH`

### 4. Executar

```bash
# Instalar dependências
go mod tidy

# Rodar
go run main.go
```

**Logs esperados:**
```
✅ Configuração carregada
✅ Conexão com PostgreSQL estabelecida
✅ Firebase inicializado com sucesso
✅ Scheduler iniciado (monitorando chamadas)
🌐 Servidor rodando em http://0.0.0.0:8080
```

---

## 🔍 Endpoints

### Health Check
```bash
curl http://localhost:8080/health
```

**Resposta:**
```json
{"status":"healthy","timestamp":"2025-12-29T10:30:00Z"}
```

### Estatísticas
```bash
curl http://localhost:8080/stats
```

**Resposta:**
```json
{
  "scheduler": {
    "agendamentos_pendentes": 5,
    "chamadas_perdidas_hoje": 2
  },
  "database": {
    "open_connections": 3,
    "in_use": 1,
    "idle": 2
  }
}
```

---

## 📊 Tabelas do Banco de Dados

### Necessárias (já existem no seu banco)

- `idosos` - Dados dos idosos
- `cuidadores` - Familiares/cuidadores (campo `device_token` **obrigatório**)
- `agendamentos` - Chamadas programadas
- `historico_ligacoes` - Registro de conversas
- `alertas` - Todos os alertas enviados
- `timeline` - Linha do tempo de eventos
- `historico_medicamentos` - Confirmações de remédios

---

## 🚨 Tipos de Alerta

### 1. Alerta de Emergência
**Trigger:** IA detecta risco na conversa  
**Arquivo:** `tools.go` → `AlertFamily()`  
**Exemplo:**
```go
AlertFamily(db, pushService, idosoID, "Paciente relatou dor no peito")
```

**Notificação:**
```
⚠️ ALERTA CRÍTICO: EVA
Maria precisa de ajuda: Paciente relatou dor no peito
```

---

### 2. Chamada Não Atendida
**Trigger:** Idoso não responde em 60 segundos  
**Arquivo:** `scheduler.go` → `checkMissedCalls()`  
**Comportamento:**
- Verifica a cada 30 segundos
- Marca agendamento como `nao_atendido`
- Registra no histórico
- Cria alerta
- Notifica TODOS os cuidadores

**Notificação:**
```
⚠️ Chamada Não Atendida
Maria não atendeu a chamada da EVA. Verifique se está tudo bem.
```

---

### 3. Confirmação de Medicamento
**Trigger:** Idoso confirma que tomou remédio  
**Arquivo:** `tools.go` → `ConfirmMedication()`  

**Notificação:**
```
✅ Medicamento Confirmado
Maria tomou o remédio: Losartana 50mg
```

---

## ⚙️ Configurações Importantes

### Intervalo do Scheduler
```bash
SCHEDULER_INTERVAL=30  # segundos (mínimo 10, recomendado 30)
```

### Timeout de Chamada
```go
// scheduler.go - linha 88
WHERE a.data_hora_agendada < (NOW() - INTERVAL '60 seconds')
```

Para mudar o timeout:
```go
WHERE a.data_hora_agendada < (NOW() - INTERVAL '120 seconds') // 2 minutos
```

---

## 🐛 Troubleshooting

### ❌ "Firebase não inicializado"
**Causa:** Arquivo `serviceAccountKey.json` ausente ou inválido  
**Solução:**
```bash
ls -la serviceAccountKey.json
# Se não existir, baixe do Firebase Console
```

### ❌ "Nenhum cuidador registrado"
**Causa:** Tabela `cuidadores` sem `device_token`  
**Solução:**
```sql
SELECT id, nome, device_token FROM cuidadores WHERE idoso_id = 1;
-- Se device_token for NULL, atualize:
UPDATE cuidadores SET device_token = 'TOKEN_DO_APP' WHERE id = 1;
```

### ❌ Alertas não chegam
**Checklist:**
1. Firebase está rodando? (check logs)
2. `device_token` está correto?
3. App Android tem permissões de notificação?
4. Testar manualmente:
   ```bash
   curl -X POST https://fcm.googleapis.com/v1/projects/YOUR_PROJECT/messages:send \
     -H "Authorization: Bearer $(gcloud auth print-access-token)" \
     -d '{"message":{"token":"DEVICE_TOKEN","notification":{"title":"Test","body":"Test"}}}'
   ```

---

## 📈 Monitoramento em Produção

### Logs Importantes

```bash
# Alertas enviados
grep "🚨" logs/eva-mind.log

# Chamadas perdidas
grep "⚠️ CHAMADA PERDIDA" logs/eva-mind.log

# Erros Firebase
grep "❌.*Firebase" logs/eva-mind.log
```

### Métricas

```bash
# Alertas últimas 24h
SELECT COUNT(*) FROM alertas WHERE criado_em > NOW() - INTERVAL '24 hours';

# Taxa de atendimento
SELECT 
  COUNT(CASE WHEN status = 'concluido' THEN 1 END) * 100.0 / COUNT(*) as taxa_atendimento
FROM agendamentos
WHERE DATE(data_hora_agendada) = CURRENT_DATE;
```

---

## 🔒 Segurança

### Permissões de Arquivos
```bash
chmod 600 .env serviceAccountKey.json
```

### Variáveis Sensíveis
Nunca commite:
- `.env`
- `serviceAccountKey.json`
- Logs com tokens

### `.gitignore`
```
.env
serviceAccountKey.json
*.log
```

---

## 🚧 Melhorias Futuras

### Prioridade Alta
1. **Fallback em Cascata**
   - Push → SMS → Email → Ligação
   
2. **Confirmação de Leitura**
   - Endpoint `/api/alerts/:id/acknowledge`
   - Tracking se cuidador viu

3. **Múltiplos Contatos**
   - Tabela `contatos_emergencia` com prioridades

### Prioridade Média
4. **Escalação Automática**
   - Se alerta crítico não visto em 5 min → Liga para telefone fixo

5. **Analytics**
   - Dashboard com métricas em tempo real

---

## 📞 Suporte

**Logs de Erro:**
```bash
tail -f logs/eva-mind.log | grep "❌"
```

**Testar Conexões:**
```bash
# Banco
psql -h 34.89.62.186 -U postgres -d eva -c "SELECT COUNT(*) FROM idosos;"

# Firebase
# (verificar no console)
```

---

## 📝 Changelog

### v2.0.0 (2025-12-29)
- ✅ Sistema de alertas completamente funcional
- ✅ Firebase FCM integrado
- ✅ Watchdog para chamadas perdidas
- ✅ Análise automática de conversas
- ✅ Múltiplos cuidadores suportados
- ✅ Health check e stats endpoints

---

## 📜 Licença

Propriedade da EVA-Mind. Todos os direitos reservados.