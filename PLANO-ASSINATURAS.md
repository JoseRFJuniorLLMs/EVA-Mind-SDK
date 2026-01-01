# 💳 Sistema de Assinaturas/Planos - EVA-Mind

**Data:** 30 de Dezembro de 2025  
**Status:** 🔴 NÃO IMPLEMENTADO

---

## 📊 Resposta às Perguntas

### **1. Existe tabela para isso?**
❌ **NÃO** - Não existe nenhuma tabela de:
- Planos/Assinaturas
- Pagamentos
- Histórico de transações
- Controle de features por plano

### **2. Bloqueios são feitos no backend?**
✅ **SIM** - Todo controle DEVE ser feito no backend por segurança:
- Verificação de plano ativo
- Validação de features permitidas
- Bloqueio de funcionalidades expiradas
- Middleware de autorização

---

## 🎯 Planos Definidos

### **Plano 1: Livre (R$ 0/mês)**
**Features:**
- ✅ Interface acessível e intuitiva
- ✅ Cadastro do perfil do idoso
- ✅ Histórico completo de chamadas
- ✅ Botão "Ligar Agora" manual

**Limitações:**
- ❌ Sem lembretes automáticos
- ❌ Sem detecção de emergências
- ❌ Sem alertas automáticos
- ❌ Sem relatórios

---

### **Plano 2: Essencial (R$ 7,99/mês)**
**Features do Livre +**
- ✅ Lembretes automáticos de remédios
- ✅ Confirmação de tomada da medicação
- ✅ Personalização de áudio (volume adaptado)
- ✅ Alertas básicos de "Não Atendeu"

**Limitações:**
- ❌ Sem detecção de emergências
- ❌ Sem monitoramento em tempo real
- ❌ Sem relatórios detalhados

---

### **Plano 3: Família+ (R$ 23/mês)**
**Features do Essencial +**
- ✅ Detecção de emergências (dor, quedas)
- ✅ Alertas vermelhos imediatos
- ✅ Monitoramento em tempo real
- ✅ Relatórios detalhados de adesão
- ✅ IA Avançada (padrões, predições)

**Limitações:**
- ❌ Limitado a 1 idoso
- ❌ Sem API de integração
- ❌ Sem suporte prioritário

---

### **Plano 4: Profissional (R$ 229/mês)**
**Features do Família+ +**
- ✅ Múltiplos idosos ilimitados
- ✅ Integrações futuras (Sensores, Smartwatch)
- ✅ Lembretes de consultas médicas
- ✅ Dados seguros e privados (HIPAA ready)
- ✅ Suporte prioritário dedicado
- ✅ API de integração
- ✅ Dashboard administrativo

---

## 🗄️ Estrutura de Banco de Dados

### **Tabela 1: `planos`**
```sql
CREATE TABLE planos (
    id SERIAL PRIMARY KEY,
    nome VARCHAR(50) NOT NULL UNIQUE, -- 'livre', 'essencial', 'familia_plus', 'profissional'
    nome_exibicao VARCHAR(100) NOT NULL, -- 'Livre', 'Essencial', 'Família+', 'Profissional'
    descricao TEXT,
    preco_mensal DECIMAL(10,2) NOT NULL,
    preco_anual DECIMAL(10,2),
    
    -- Limites
    max_idosos INTEGER DEFAULT 1, -- NULL = ilimitado
    max_cuidadores INTEGER DEFAULT 5,
    max_chamadas_mes INTEGER, -- NULL = ilimitado
    
    -- Features (JSON para flexibilidade)
    features JSONB NOT NULL DEFAULT '{}',
    
    ativo BOOLEAN DEFAULT TRUE,
    ordem_exibicao INTEGER DEFAULT 0,
    criado_em TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    atualizado_em TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Dados iniciais
INSERT INTO planos (nome, nome_exibicao, preco_mensal, preco_anual, max_idosos, features, ordem_exibicao) VALUES
('livre', 'Livre', 0.00, 0.00, 1, '{
    "interface_acessivel": true,
    "cadastro_idoso": true,
    "historico_chamadas": true,
    "ligar_agora_manual": true,
    "lembretes_automaticos": false,
    "confirmacao_medicacao": false,
    "personalizacao_audio": false,
    "alertas_nao_atendeu": false,
    "deteccao_emergencias": false,
    "monitoramento_tempo_real": false,
    "relatorios_detalhados": false,
    "ia_avancada": false,
    "api_integracao": false,
    "suporte_prioritario": false
}', 1),

('essencial', 'Essencial', 7.99, 79.90, 1, '{
    "interface_acessivel": true,
    "cadastro_idoso": true,
    "historico_chamadas": true,
    "ligar_agora_manual": true,
    "lembretes_automaticos": true,
    "confirmacao_medicacao": true,
    "personalizacao_audio": true,
    "alertas_nao_atendeu": true,
    "deteccao_emergencias": false,
    "monitoramento_tempo_real": false,
    "relatorios_detalhados": false,
    "ia_avancada": false,
    "api_integracao": false,
    "suporte_prioritario": false
}', 2),

('familia_plus', 'Família+', 23.00, 230.00, 1, '{
    "interface_acessivel": true,
    "cadastro_idoso": true,
    "historico_chamadas": true,
    "ligar_agora_manual": true,
    "lembretes_automaticos": true,
    "confirmacao_medicacao": true,
    "personalizacao_audio": true,
    "alertas_nao_atendeu": true,
    "deteccao_emergencias": true,
    "monitoramento_tempo_real": true,
    "relatorios_detalhados": true,
    "ia_avancada": true,
    "api_integracao": false,
    "suporte_prioritario": false
}', 3),

('profissional', 'Profissional', 229.00, 2290.00, NULL, '{
    "interface_acessivel": true,
    "cadastro_idoso": true,
    "historico_chamadas": true,
    "ligar_agora_manual": true,
    "lembretes_automaticos": true,
    "confirmacao_medicacao": true,
    "personalizacao_audio": true,
    "alertas_nao_atendeu": true,
    "deteccao_emergencias": true,
    "monitoramento_tempo_real": true,
    "relatorios_detalhados": true,
    "ia_avancada": true,
    "api_integracao": true,
    "suporte_prioritario": true,
    "idosos_ilimitados": true,
    "integracao_sensores": true,
    "lembretes_consultas": true,
    "hipaa_ready": true
}', 4);
```

---

### **Tabela 2: `assinaturas`**
```sql
CREATE TABLE assinaturas (
    id SERIAL PRIMARY KEY,
    usuario_id INTEGER, -- FK para tabela de usuários (a criar)
    plano_id INTEGER NOT NULL REFERENCES planos(id),
    
    -- Status
    status VARCHAR(20) NOT NULL CHECK (status IN ('ativa', 'cancelada', 'expirada', 'suspensa', 'trial')),
    
    -- Datas
    data_inicio TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    data_fim TIMESTAMP, -- NULL = indeterminado
    data_proxima_cobranca TIMESTAMP,
    data_cancelamento TIMESTAMP,
    
    -- Pagamento
    periodicidade VARCHAR(20) DEFAULT 'mensal' CHECK (periodicidade IN ('mensal', 'anual')),
    valor_pago DECIMAL(10,2),
    
    -- Trial
    eh_trial BOOLEAN DEFAULT FALSE,
    dias_trial INTEGER DEFAULT 7,
    
    -- Controle
    auto_renovar BOOLEAN DEFAULT TRUE,
    motivo_cancelamento TEXT,
    
    criado_em TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    atualizado_em TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Garantir que cada usuário tenha apenas uma assinatura ativa
    CONSTRAINT unique_assinatura_ativa UNIQUE (usuario_id, status) 
        WHERE status = 'ativa'
);

CREATE INDEX idx_assinaturas_usuario ON assinaturas(usuario_id);
CREATE INDEX idx_assinaturas_status ON assinaturas(status);
CREATE INDEX idx_assinaturas_proxima_cobranca ON assinaturas(data_proxima_cobranca) 
    WHERE status = 'ativa';
```

---

### **Tabela 3: `pagamentos`**
```sql
CREATE TABLE pagamentos (
    id SERIAL PRIMARY KEY,
    assinatura_id INTEGER NOT NULL REFERENCES assinaturas(id),
    
    -- Dados do pagamento
    valor DECIMAL(10,2) NOT NULL,
    metodo_pagamento VARCHAR(50), -- 'cartao_credito', 'pix', 'boleto'
    status VARCHAR(20) NOT NULL CHECK (status IN ('pendente', 'aprovado', 'recusado', 'estornado', 'cancelado')),
    
    -- Gateway de pagamento
    gateway VARCHAR(50), -- 'stripe', 'mercadopago', 'pagseguro'
    gateway_transaction_id VARCHAR(255),
    gateway_response JSONB,
    
    -- Datas
    data_pagamento TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    data_aprovacao TIMESTAMP,
    data_vencimento TIMESTAMP,
    
    -- Informações adicionais
    descricao TEXT,
    nota_fiscal VARCHAR(255),
    
    criado_em TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    atualizado_em TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_pagamentos_assinatura ON pagamentos(assinatura_id);
CREATE INDEX idx_pagamentos_status ON pagamentos(status);
CREATE INDEX idx_pagamentos_gateway_id ON pagamentos(gateway_transaction_id);
```

---

### **Tabela 4: `usuarios`** (se não existir)
```sql
CREATE TABLE IF NOT EXISTS usuarios (
    id SERIAL PRIMARY KEY,
    nome VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    senha_hash VARCHAR(255) NOT NULL,
    telefone VARCHAR(20),
    
    -- Tipo de usuário
    tipo VARCHAR(20) DEFAULT 'cuidador' CHECK (tipo IN ('cuidador', 'admin', 'profissional')),
    
    -- Status
    ativo BOOLEAN DEFAULT TRUE,
    email_verificado BOOLEAN DEFAULT FALSE,
    
    -- Dados adicionais
    cpf VARCHAR(14),
    data_nascimento DATE,
    
    criado_em TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    atualizado_em TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_usuarios_email ON usuarios(email);
CREATE INDEX idx_usuarios_tipo ON usuarios(tipo);
```

---

### **Tabela 5: `usuarios_idosos`** (Relacionamento)
```sql
CREATE TABLE usuarios_idosos (
    id SERIAL PRIMARY KEY,
    usuario_id INTEGER NOT NULL REFERENCES usuarios(id),
    idoso_id INTEGER NOT NULL REFERENCES idosos(id),
    
    -- Permissões
    eh_responsavel BOOLEAN DEFAULT FALSE,
    pode_editar BOOLEAN DEFAULT TRUE,
    pode_visualizar BOOLEAN DEFAULT TRUE,
    
    criado_em TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    CONSTRAINT unique_usuario_idoso UNIQUE(usuario_id, idoso_id)
);

CREATE INDEX idx_usuarios_idosos_usuario ON usuarios_idosos(usuario_id);
CREATE INDEX idx_usuarios_idosos_idoso ON usuarios_idosos(idoso_id);
```

---

## 🔧 Implementação Backend

### **1. Middleware de Verificação de Plano**

```go
// internal/middleware/subscription.go
package middleware

import (
    "context"
    "database/sql"
    "encoding/json"
    "net/http"
)

type SubscriptionMiddleware struct {
    db *sql.DB
}

func NewSubscriptionMiddleware(db *sql.DB) *SubscriptionMiddleware {
    return &SubscriptionMiddleware{db: db}
}

// RequireFeature verifica se o usuário tem acesso à feature
func (sm *SubscriptionMiddleware) RequireFeature(feature string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Pegar usuário do contexto (após autenticação)
            userID := r.Context().Value("user_id").(int)
            
            // Verificar plano ativo
            hasFeature, err := sm.checkFeature(userID, feature)
            if err != nil || !hasFeature {
                http.Error(w, "Feature não disponível no seu plano", http.StatusForbidden)
                return
            }
            
            next.ServeHTTP(w, r)
        })
    }
}

func (sm *SubscriptionMiddleware) checkFeature(userID int, feature string) (bool, error) {
    query := `
        SELECT p.features
        FROM assinaturas a
        JOIN planos p ON p.id = a.plano_id
        WHERE a.usuario_id = $1
          AND a.status = 'ativa'
          AND (a.data_fim IS NULL OR a.data_fim > NOW())
        LIMIT 1
    `
    
    var featuresJSON []byte
    err := sm.db.QueryRow(query, userID).Scan(&featuresJSON)
    if err != nil {
        return false, err
    }
    
    var features map[string]bool
    if err := json.Unmarshal(featuresJSON, &features); err != nil {
        return false, err
    }
    
    return features[feature], nil
}

// CheckSubscriptionStatus verifica se assinatura está ativa
func (sm *SubscriptionMiddleware) CheckSubscriptionStatus(userID int) (string, error) {
    query := `
        SELECT status
        FROM assinaturas
        WHERE usuario_id = $1
        ORDER BY criado_em DESC
        LIMIT 1
    `
    
    var status string
    err := sm.db.QueryRow(query, userID).Scan(&status)
    return status, err
}
```

---

### **2. Service de Assinaturas**

```go
// internal/subscription/service.go
package subscription

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "time"
)

type SubscriptionService struct {
    db *sql.DB
}

type Plan struct {
    ID             int
    Nome           string
    NomeExibicao   string
    PrecoMensal    float64
    PrecoAnual     float64
    MaxIdosos      *int
    Features       map[string]bool
}

type Subscription struct {
    ID                  int
    UsuarioID           int
    PlanoID             int
    Status              string
    DataInicio          time.Time
    DataFim             *time.Time
    DataProximaCobranca *time.Time
    Periodicidade       string
    ValorPago           float64
}

func NewSubscriptionService(db *sql.DB) *SubscriptionService {
    return &SubscriptionService{db: db}
}

// GetActivePlans retorna todos os planos ativos
func (ss *SubscriptionService) GetActivePlans() ([]Plan, error) {
    query := `
        SELECT id, nome, nome_exibicao, preco_mensal, preco_anual, max_idosos, features
        FROM planos
        WHERE ativo = true
        ORDER BY ordem_exibicao
    `
    
    rows, err := ss.db.Query(query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var plans []Plan
    for rows.Next() {
        var p Plan
        var featuresJSON []byte
        var maxIdosos sql.NullInt64
        
        err := rows.Scan(&p.ID, &p.Nome, &p.NomeExibicao, &p.PrecoMensal, 
                        &p.PrecoAnual, &maxIdosos, &featuresJSON)
        if err != nil {
            continue
        }
        
        if maxIdosos.Valid {
            val := int(maxIdosos.Int64)
            p.MaxIdosos = &val
        }
        
        json.Unmarshal(featuresJSON, &p.Features)
        plans = append(plans, p)
    }
    
    return plans, nil
}

// CreateSubscription cria nova assinatura
func (ss *SubscriptionService) CreateSubscription(userID, planID int, periodicidade string) (*Subscription, error) {
    // Buscar preço do plano
    var preco float64
    if periodicidade == "anual" {
        ss.db.QueryRow("SELECT preco_anual FROM planos WHERE id = $1", planID).Scan(&preco)
    } else {
        ss.db.QueryRow("SELECT preco_mensal FROM planos WHERE id = $1", planID).Scan(&preco)
    }
    
    // Calcular próxima cobrança
    proximaCobranca := time.Now()
    if periodicidade == "anual" {
        proximaCobranca = proximaCobranca.AddDate(1, 0, 0)
    } else {
        proximaCobranca = proximaCobranca.AddDate(0, 1, 0)
    }
    
    query := `
        INSERT INTO assinaturas (
            usuario_id, plano_id, status, periodicidade, 
            valor_pago, data_proxima_cobranca
        ) VALUES ($1, $2, 'ativa', $3, $4, $5)
        RETURNING id, data_inicio
    `
    
    var sub Subscription
    err := ss.db.QueryRow(query, userID, planID, periodicidade, preco, proximaCobranca).
        Scan(&sub.ID, &sub.DataInicio)
    
    if err != nil {
        return nil, err
    }
    
    sub.UsuarioID = userID
    sub.PlanoID = planID
    sub.Status = "ativa"
    sub.Periodicidade = periodicidade
    sub.ValorPago = preco
    sub.DataProximaCobranca = &proximaCobranca
    
    return &sub, nil
}

// CancelSubscription cancela assinatura
func (ss *SubscriptionService) CancelSubscription(subscriptionID int, motivo string) error {
    query := `
        UPDATE assinaturas
        SET status = 'cancelada',
            data_cancelamento = NOW(),
            motivo_cancelamento = $1,
            auto_renovar = false
        WHERE id = $2
    `
    
    _, err := ss.db.Exec(query, motivo, subscriptionID)
    return err
}

// CheckExpiredSubscriptions verifica e expira assinaturas vencidas
func (ss *SubscriptionService) CheckExpiredSubscriptions() error {
    query := `
        UPDATE assinaturas
        SET status = 'expirada'
        WHERE status = 'ativa'
          AND data_proxima_cobranca < NOW()
          AND auto_renovar = false
    `
    
    _, err := ss.db.Exec(query)
    return err
}
```

---

### **3. APIs REST**

```go
// Adicionar em main.go ou em um router separado

// GET /api/plans - Listar planos
api.HandleFunc("/plans", plansHandler).Methods("GET")

// GET /api/subscription - Ver assinatura atual
api.HandleFunc("/subscription", getSubscriptionHandler).Methods("GET")

// POST /api/subscription - Criar/Atualizar assinatura
api.HandleFunc("/subscription", createSubscriptionHandler).Methods("POST")

// DELETE /api/subscription - Cancelar assinatura
api.HandleFunc("/subscription", cancelSubscriptionHandler).Methods("DELETE")

// POST /api/subscription/upgrade - Fazer upgrade de plano
api.HandleFunc("/subscription/upgrade", upgradeSubscriptionHandler).Methods("POST")

// GET /api/subscription/features - Verificar features disponíveis
api.HandleFunc("/subscription/features", getFeaturesHandler).Methods("GET")
```

---

## 🔒 Controle de Features no Código

### **Exemplo: Bloquear Detecção de Emergências**

```go
// Em internal/gemini/tools.go

func AlertFamilyWithSeverity(...) {
    // Verificar se usuário tem feature de detecção de emergências
    hasFeature, _ := subscriptionService.CheckFeature(userID, "deteccao_emergencias")
    
    if !hasFeature && severity == "critica" {
        log.Printf("⚠️ Usuário não tem acesso a detecção de emergências (plano insuficiente)")
        return fmt.Errorf("feature não disponível no seu plano")
    }
    
    // Continuar com lógica normal...
}
```

### **Exemplo: Bloquear IA Avançada**

```go
// Em internal/workers/pattern_worker.go

func (pw *PatternWorker) Run(ctx context.Context) error {
    // Para cada idoso, verificar se o responsável tem IA avançada
    for _, idosoID := range idosos {
        userID := getResponsavelID(idosoID)
        hasFeature, _ := subscriptionService.CheckFeature(userID, "ia_avancada")
        
        if !hasFeature {
            log.Printf("⏭️ Pulando análise de IA para idoso %d (plano insuficiente)", idosoID)
            continue
        }
        
        // Executar análise de padrões...
    }
}
```

---

## 💳 Integração com Gateway de Pagamento

### **Opções Recomendadas:**

1. **Stripe** (Internacional)
   - Mais completo
   - Suporte a assinaturas
   - Webhooks automáticos

2. **Mercado Pago** (Brasil)
   - PIX integrado
   - Boleto
   - Cartão de crédito

3. **PagSeguro** (Brasil)
   - Fácil integração
   - Suporte local

### **Webhook para Renovação Automática:**

```go
// POST /api/webhooks/payment
func paymentWebhookHandler(w http.ResponseWriter, r *http.Request) {
    // Verificar assinatura do webhook
    
    // Processar evento
    switch event.Type {
    case "payment.approved":
        // Renovar assinatura
        subscriptionService.RenewSubscription(event.SubscriptionID)
        
    case "payment.failed":
        // Suspender assinatura
        subscriptionService.SuspendSubscription(event.SubscriptionID)
        
    case "subscription.cancelled":
        // Cancelar assinatura
        subscriptionService.CancelSubscription(event.SubscriptionID, "cancelado pelo gateway")
    }
}
```

---

## 📊 Cronograma de Implementação

### **Fase 1: Banco de Dados (1 dia)**
- [ ] Criar migration com 5 tabelas
- [ ] Popular tabela de planos
- [ ] Testar constraints

### **Fase 2: Backend Core (2-3 dias)**
- [ ] Criar SubscriptionService
- [ ] Criar middleware de verificação
- [ ] Integrar com código existente

### **Fase 3: APIs (1-2 dias)**
- [ ] Implementar endpoints REST
- [ ] Testes de API

### **Fase 4: Integração Pagamento (3-4 dias)**
- [ ] Escolher gateway
- [ ] Implementar webhooks
- [ ] Testes de pagamento

### **Fase 5: Frontend (1 semana)**
- [ ] Página de planos
- [ ] Checkout
- [ ] Dashboard de assinatura

---

## 🎯 Próximos Passos

1. **Decidir gateway de pagamento** (Stripe, Mercado Pago, PagSeguro)
2. **Criar migration de banco de dados**
3. **Implementar SubscriptionService**
4. **Adicionar middleware de verificação**
5. **Testar bloqueios de features**

---

**Estimativa Total:** 2-3 semanas  
**Complexidade:** Alta  
**Prioridade:** Alta (necessário para monetização)
