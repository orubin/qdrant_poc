package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/a-h/templ"
	"qdrant-poc/internal/gemini"
	"qdrant-poc/internal/qdrant"
	"qdrant-poc/internal/ui"
	"qdrant-poc/pkg/models"
)

type App struct {
	qdrant *qdrant.Service
	gemini *gemini.Service
	
	logs      []models.ActionLog
	logsMu    sync.RWMutex
	
	messages  []models.ChatMessage
	msgMu     sync.RWMutex
}

func NewApp(qdrantSvc *qdrant.Service, geminiSvc *gemini.Service) *App {
	return &App{
		qdrant: qdrantSvc,
		gemini: geminiSvc,
		logs:   make([]models.ActionLog, 0),
		messages: []models.ChatMessage{
			{Role: "assistant", Content: "Hello! I'm your RAG assistant. Ask me anything about the docs we've indexed."},
		},
	}
}

func (a *App) addLog(action, details string) {
	a.logsMu.Lock()
	defer a.logsMu.Unlock()
	a.logs = append([]models.ActionLog{{
		Timestamp: time.Now().Format("15:04:05"),
		Action:    action,
		Details:   details,
	}}, a.logs...)
}

func (a *App) HandleIndex(w http.ResponseWriter, r *http.Request) {
	a.logsMu.RLock()
	a.msgMu.RLock()
	defer a.logsMu.RUnlock()
	defer a.msgMu.RUnlock()

	chatWindow := ui.ChatWindow(a.messages)
	actionLogs := ui.ActionLogs(a.logs)
	templ.Handler(ui.Dashboard(chatWindow, actionLogs)).ServeHTTP(w, r)
}

func (a *App) HandleChat(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	userMsg := r.FormValue("message")
	if userMsg == "" {
		return
	}

	a.msgMu.Lock()
	a.messages = append(a.messages, models.ChatMessage{Role: "user", Content: userMsg})
	a.msgMu.Unlock()

	// Render user message immediately
	templ.Handler(ui.Message(models.ChatMessage{Role: "user", Content: userMsg})).ServeHTTP(w, r)

	go a.processRAG(userMsg)
}

func (a *App) HandleChatMessages(w http.ResponseWriter, r *http.Request) {
	a.msgMu.RLock()
	defer a.msgMu.RUnlock()
	
	templ.Handler(ui.ChatMessages(a.messages)).ServeHTTP(w, r)
}

func (a *App) HandleLogs(w http.ResponseWriter, r *http.Request) {
	a.logsMu.RLock()
	defer a.logsMu.RUnlock()
	
	for _, l := range a.logs {
		templ.Handler(ui.LogItem(l)).ServeHTTP(w, r)
	}
}

func (a *App) processRAG(query string) {
	ctx := context.Background()
	
	a.addLog("RAG Start", fmt.Sprintf("Processing query: %s", query))
	
	// 1. Generate Embedding
	a.addLog("Embedding", "Generating embedding via Gemini...")
	vector, err := a.gemini.GenerateEmbedding(ctx, query)
	if err != nil {
		a.addLog("Error", fmt.Sprintf("Embedding failed: %v", err))
		return
	}
	
	// 2. Search Qdrant
	a.addLog("Qdrant Search", "Searching for relevant context...")
	results, err := a.qdrant.Search(ctx, "tech-docs", vector, 3)
	if err != nil {
		a.addLog("Error", fmt.Sprintf("Search failed: %v", err))
		return
	}
	
	contextItems := make([]string, 0)
	for _, res := range results {
		if textVal, ok := res.Payload["text"]; ok {
			contextItems = append(contextItems, textVal.GetStringValue())
		}
	}
	a.addLog("Context Found", fmt.Sprintf("Retrieved %d snippets from Qdrant", len(contextItems)))
	
	// 3. Generate Response
	a.addLog("Gemini Generate", "Generating final response with context...")
	response, err := a.gemini.GenerateResponse(ctx, query, contextItems)
	if err != nil {
		a.addLog("Error", fmt.Sprintf("Generation failed: %v", err))
		return
	}
	
	a.msgMu.Lock()
	a.messages = append(a.messages, models.ChatMessage{Role: "assistant", Content: response})
	a.msgMu.Unlock()
	
	a.addLog("RAG Complete", "Assistant response generated successfully")
}
