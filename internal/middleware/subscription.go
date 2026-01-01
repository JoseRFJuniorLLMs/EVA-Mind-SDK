package middleware

import (
	"encoding/json"
	"eva-mind/internal/subscription"
	"log"
	"net/http"
)

// SubscriptionMiddleware gerencia verificação de features
type SubscriptionMiddleware struct {
	subscriptionService *subscription.SubscriptionService
}

// NewSubscriptionMiddleware cria nova instância do middleware
func NewSubscriptionMiddleware(service *subscription.SubscriptionService) *SubscriptionMiddleware {
	return &SubscriptionMiddleware{
		subscriptionService: service,
	}
}

// RequireFeature retorna um middleware que verifica se a entidade tem acesso à feature
func (sm *SubscriptionMiddleware) RequireFeature(feature string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Pegar nome da entidade do query parameter ou header
			entityName := r.URL.Query().Get("entity")
			if entityName == "" {
				entityName = r.Header.Get("X-Entity-Name")
			}

			if entityName == "" {
				http.Error(w, "Nome da entidade não fornecido", http.StatusBadRequest)
				return
			}

			// Verificar se tem acesso à feature
			hasFeature, err := sm.subscriptionService.CheckFeature(entityName, feature)
			if err != nil {
				log.Printf("❌ Erro ao verificar feature '%s' para %s: %v", feature, entityName, err)
				http.Error(w, "Erro ao verificar permissões", http.StatusInternalServerError)
				return
			}

			if !hasFeature {
				log.Printf("🚫 Acesso negado: %s não tem feature '%s'", entityName, feature)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "Feature não disponível",
					"message": "Esta funcionalidade não está disponível no seu plano atual",
					"feature": feature,
				})
				return
			}

			// Feature disponível, continuar
			next.ServeHTTP(w, r)
		})
	}
}

// CheckFeatureAccess verifica se uma entidade tem acesso a uma feature (função auxiliar)
func (sm *SubscriptionMiddleware) CheckFeatureAccess(entityName, feature string) (bool, error) {
	return sm.subscriptionService.CheckFeature(entityName, feature)
}
