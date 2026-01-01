package gemini

import (
	"bytes"
	"encoding/json"
	"eva-mind/internal/config"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ConversationAnalysis resultado completo da análise
type ConversationAnalysis struct {
	// Saúde Física
	ReportedPain      bool   `json:"reported_pain"`
	PainLocation      string `json:"pain_location"`
	PainIntensity     int    `json:"pain_intensity"` // 0-10
	EmergencySymptoms bool   `json:"emergency_symptoms"`
	EmergencyType     string `json:"emergency_type"` // "infarto", "avc", "queda", "respiratorio", ""

	// Saúde Mental
	MoodState  string `json:"mood_state"` // "feliz", "triste", "ansioso", "confuso", "irritado", "neutro"
	Depression bool   `json:"depression"`
	Confusion  bool   `json:"confusion"`
	Loneliness bool   `json:"loneliness"`

	// Medicação
	MedicationTaken  bool `json:"medication_taken"`
	MedicationIssues bool `json:"medication_issues"`
	SideEffects      bool `json:"side_effects"`

	// Urgência
	UrgencyLevel      string `json:"urgency_level"` // "CRITICO", "ALTO", "MEDIO", "BAIXO"
	RecommendedAction string `json:"recommended_action"`

	// Resumo
	Summary     string   `json:"summary"`
	KeyConcerns []string `json:"key_concerns"`

	// Campos extras para controle interno (não vem do Gemini)
	LastAnalysisAt time.Time `json:"last_analysis_at,omitempty"`
}

// AnalyzeConversation analisa a conversa e retorna o struct
func AnalyzeConversation(cfg *config.Config, transcription string) (*ConversationAnalysis, error) {
	cleanedTranscript := cleanTranscription(transcription)
	if strings.TrimSpace(cleanedTranscript) == "" {
		return nil, fmt.Errorf("transcrição vazia após limpeza")
	}

	prompt := fmt.Sprintf(`Você é um médico especialista em gerontologia e psicologia. Analise esta conversa com um idoso e identifique:

CONVERSA:
%s

Responda APENAS com um JSON válido (sem markdown, sem explicações) seguindo exatamente esta estrutura:

{
  "reported_pain": true/false,
  "pain_location": "localização exata ou vazio",
  "pain_intensity": 0-10,
  "emergency_symptoms": true/false,
  "emergency_type": "infarto/avc/queda/respiratorio ou vazio",
  "mood_state": "feliz/triste/ansioso/confuso/irritado/neutro",
  "depression": true/false,
  "confusion": true/false,
  "loneliness": true/false,
  "medication_taken": true/false,
  "medication_issues": true/false,
  "side_effects": true/false,
  "urgency_level": "CRITICO/ALTO/MEDIO/BAIXO",
  "recommended_action": "descrição breve da ação recomendada",
  "summary": "resumo clínico em 2-3 linhas",
  "key_concerns": ["preocupação 1", "preocupação 2"]
}

CRITÉRIOS DE URGÊNCIA:
- CRÍTICO: Dor no peito, falta de ar severa, confusão súbita, queda com trauma, AVC
- ALTO: Dor persistente, depressão severa, recusa de medicação
- MÉDIO: Tristeza, solidão, desconforto leve
- BAIXO: Conversa normal, sem queixas

Seja objetivo e preciso. Se não tiver informação, use false/vazio/0.`, cleanedTranscript)

	model := cfg.GeminiAnalysisModel
	if model == "" {
		model = "gemini-2.5-flash"
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent?key=%s", model, cfg.GoogleAPIKey)

	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":     0.1,
			"maxOutputTokens": 2048,
		},
	}

	jsonPayload, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("falha ao chamar Gemini API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("Gemini API retornou status %d: %v", resp.StatusCode, errResp)
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("falha ao decodificar resposta: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("resposta vazia do Gemini")
	}

	responseText := result.Candidates[0].Content.Parts[0].Text
	responseText = strings.TrimSpace(responseText)
	responseText = strings.TrimPrefix(responseText, "```json")
	responseText = strings.TrimPrefix(responseText, "```")
	responseText = strings.TrimSuffix(responseText, "```")
	responseText = strings.TrimSpace(responseText)

	var analysis ConversationAnalysis
	if err := json.Unmarshal([]byte(responseText), &analysis); err != nil {
		return nil, fmt.Errorf("falha ao parsear análise: %w (resposta: %s)", err, responseText)
	}

	// Adiciona timestamp da análise
	analysis.LastAnalysisAt = time.Now()

	return &analysis, nil
}

// cleanTranscription (mantida igual, mas agora usada em AnalyzeConversation)
func cleanTranscription(transcript string) string {
	var extracted []string
	text := transcript

	for {
		start := strings.Index(text, "\"\"")
		if start == -1 {
			break
		}
		text = text[start+2:]
		end := strings.Index(text, "\"\"")
		if end == -1 {
			break
		}
		content := strings.TrimSpace(text[:end])
		if content != "" && len(content) > 2 {
			extracted = append(extracted, "IDOSO: "+content)
		}
		text = text[end+2:]
	}

	result := strings.Join(extracted, "\n")
	if result == "" {
		return ""
	}
	return result
}

// AnalyzeSentiment (deprecated, mantido)
func AnalyzeSentiment(cfg *config.Config, transcription string) (string, error) {
	analysis, err := AnalyzeConversation(cfg, transcription)
	if err != nil {
		return "neutro", err
	}
	return analysis.MoodState, nil
}
