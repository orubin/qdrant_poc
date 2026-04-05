package models

import (
	"github.com/qdrant/go-client/qdrant"
)

type Point struct {
	ID      uint64                 `json:"id"`
	Vector  []float32              `json:"vector"`
	Payload map[string]interface{} `json:"payload"`
}

type SearchResult struct {
	ID      uint64                   `json:"id"`
	Score   float32                  `json:"score"`
	Payload map[string]*qdrant.Value `json:"payload"`
}

type ActionLog struct {
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"`
	Details   string `json:"details"`
}

type SourceCitation struct {
	Source string  `json:"source"`
	Score  float32 `json:"score"`
}

type ChatMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Citations []SourceCitation `json:"citations,omitempty"`
}
