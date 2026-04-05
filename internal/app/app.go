package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/google/generative-ai-go/genai"
	"qdrant-poc/internal/db"
	"qdrant-poc/internal/ui"
	"qdrant-poc/pkg/models"
)

type QdrantService interface {
	Search(ctx context.Context, collectionName string, vector []float32, limit uint64) ([]models.SearchResult, error)
	UpsertPoints(ctx context.Context, collectionName string, points []models.Point) error
	GetCollectionStatus(ctx context.Context, collectionName string) (uint64, error)
}

type GeminiService interface {
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
	GenerateResponse(ctx context.Context, prompt string, history []models.ChatMessage, contextItems []string) (string, error)
	GenerateResponseStream(ctx context.Context, prompt string, history []models.ChatMessage, contextItems []string) *genai.GenerateContentResponseIterator
}

type App struct {
	qdrant QdrantService
	gemini GeminiService
	db     *db.DB
	
	logs      []models.ActionLog
	logsMu    sync.RWMutex
	
	messages  []models.ChatMessage
	msgMu     sync.RWMutex

	idCounter uint64
	idMu      sync.Mutex
}

func NewApp(qdrantSvc QdrantService, geminiSvc GeminiService, database *db.DB) *App {
	messages, err := database.GetMessages()
	if err != nil || len(messages) == 0 {
		messages = []models.ChatMessage{
			{Role: "assistant", Content: "Hello! I'm your RAG assistant. Ask me anything about the docs we've indexed."},
		}
		database.SaveMessage(messages[0].Role, messages[0].Content, nil)
	}

	return &App{
		qdrant: qdrantSvc,
		gemini: geminiSvc,
		db:     database,
		logs:   make([]models.ActionLog, 0),
		messages: messages,
		idCounter: 100, // Start high to avoid collision with initial seed docs (1-5)
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
	a.db.SaveMessage("user", userMsg, nil)

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

func (a *App) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(10 << 20) // 10MB limit
	if err != nil {
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("document")
	if err != nil {
		http.Error(w, "failed to get file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}

	go a.processUpload(header.Filename, string(content))

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "File %s uploaded and being processed...", header.Filename)
}

func (a *App) HandleCollectionStatus(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	count, err := a.qdrant.GetCollectionStatus(ctx, "tech-docs")
	if err != nil {
		http.Error(w, "failed to get status", http.StatusInternalServerError)
		return
	}

	templ.Handler(ui.CollectionStatus(count)).ServeHTTP(w, r)
}

func (a *App) processUpload(filename, content string) {
	ctx := context.Background()
	a.addLog("Upload Started", fmt.Sprintf("Processing file: %s", filename))

	chunks := a.chunkText(content)
	a.addLog("Chunking", fmt.Sprintf("Split %s into %d chunks", filename, len(chunks)))

	for i, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}

		a.addLog("Embedding", fmt.Sprintf("Generating embedding for chunk %d/%d...", i+1, len(chunks)))
		vector, err := a.gemini.GenerateEmbedding(ctx, chunk)
		if err != nil {
			a.addLog("Error", fmt.Sprintf("Embedding failed for chunk %d: %v", i+1, err))
			continue
		}

		a.idMu.Lock()
		pointID := a.idCounter
		a.idCounter++
		a.idMu.Unlock()

		err = a.qdrant.UpsertPoints(ctx, "tech-docs", []models.Point{
			{
				ID:     pointID,
				Vector: vector,
				Payload: map[string]interface{}{
					"text":     chunk,
					"source":   filename,
					"chunk_id": i,
				},
			},
		})
		if err != nil {
			a.addLog("Error", fmt.Sprintf("Indexing failed for chunk %d: %v", i+1, err))
			continue
		}
	}

	a.addLog("Upload Complete", fmt.Sprintf("Finished indexing %s", filename))
}

func (a *App) chunkText(text string) []string {
	// Simple paragraph-based chunking
	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	for _, p := range paragraphs {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			chunks = append(chunks, trimmed)
		}
	}
	return chunks
}

func (a *App) processRAG(query string) {
	ctx := context.Background()
	
	a.addLog("RAG Start", fmt.Sprintf("Processing query: %s", query))
	
	// 0. Fetch History for Memory
	a.msgMu.RLock()
	history := make([]models.ChatMessage, 0)
	// Get last 5 messages for context
	startIdx := 0
	if len(a.messages) > 5 {
		startIdx = len(a.messages) - 5
	}
	history = append(history, a.messages[startIdx:len(a.messages)-1]...) // exclude the current user message which is passed as 'query'
	a.msgMu.RUnlock()

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
	citations := make([]models.SourceCitation, 0)
	for _, res := range results {
		if textVal, ok := res.Payload["text"]; ok {
			text := textVal.GetStringValue()
			contextItems = append(contextItems, text)
			
			source := "unknown"
			if srcVal, ok := res.Payload["source"]; ok {
				source = srcVal.GetStringValue()
			}
			citations = append(citations, models.SourceCitation{
				Source: source,
				Score:  res.Score,
			})
		}
	}
	a.addLog("Context Found", fmt.Sprintf("Retrieved %d snippets (avg score: %.2f)", len(contextItems), calculateAvgScore(citations)))
	
	// 3. Generate Response
	a.addLog("Gemini Generate", "Generating response with memory & context...")
	iter := a.gemini.GenerateResponseStream(ctx, query, history, contextItems)
	
	a.msgMu.Lock()
	msgIndex := len(a.messages)
	a.messages = append(a.messages, models.ChatMessage{Role: "assistant", Content: "typing...", Citations: citations})
	a.msgMu.Unlock()

	var fullResponse strings.Builder
	firstChunk := true
	for {
		resp, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			a.addLog("Error", fmt.Sprintf("Streaming failed: %v", err))
			return
		}

		if len(resp.Candidates) > 0 {
			part := resp.Candidates[0].Content.Parts[0]
			if text, ok := part.(genai.Text); ok {
				if firstChunk {
					fullResponse.Reset() // Remove "typing..."
					firstChunk = false
				}
				fullResponse.WriteString(string(text))
				
				a.msgMu.Lock()
				a.messages[msgIndex].Content = fullResponse.String()
				a.msgMu.Unlock()
			}
		}
	}
	
	a.db.SaveMessage("assistant", fullResponse.String(), citations)
	a.addLog("RAG Complete", "Assistant response generated and persisted")
}

func calculateAvgScore(citations []models.SourceCitation) float32 {
	if len(citations) == 0 {
		return 0
	}
	var total float32
	for _, c := range citations {
		total += c.Score
	}
	return total / float32(len(citations))
}

