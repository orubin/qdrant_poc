package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"qdrant-poc/internal/app"
	"qdrant-poc/internal/gemini"
	"qdrant-poc/internal/qdrant"
	"qdrant-poc/pkg/models"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY is required")
	}

	qHost := os.Getenv("QDRANT_HOST")
	if qHost == "" {
		qHost = "localhost"
	}
	qPortStr := os.Getenv("QDRANT_PORT")
	if qPortStr == "" {
		qPortStr = "6334"
	}
	qPort, _ := strconv.Atoi(qPortStr)

	ctx := context.Background()

	// Initialize Gemini
	geminiSvc, err := gemini.NewService(ctx, apiKey)
	if err != nil {
		log.Fatalf("failed to init gemini: %v", err)
	}
	defer geminiSvc.Close()

	// Initialize Qdrant
	qdrantSvc, err := qdrant.NewService(qHost, qPort)
	if err != nil {
		log.Fatalf("failed to init qdrant: %v", err)
	}
	defer qdrantSvc.Close()

	// Ensure collection exists and seed data
	collectionName := "tech-docs"
	exists, err := qdrantSvc.CollectionExists(ctx, collectionName)
	if err != nil {
		log.Fatalf("failed to check collection: %v", err)
	}

	if !exists {
		log.Printf("Creating collection %s...", collectionName)
		// text-embedding-004 is 768 dimensions
		if err := qdrantSvc.CreateCollection(ctx, collectionName, 768); err != nil {
			log.Fatalf("failed to create collection: %v", err)
		}

		seedDocs := []string{
			"Qdrant is a vector database and vector similarity search engine. It provides a production-ready service with a convenient API to store, search, and manage vectors with an additional payload.",
			"Go (also known as Golang) is an open-source programming language that makes it easy to build simple, reliable, and efficient software.",
			"Retrieval-Augmented Generation (RAG) is a technique that grants generative AI models access to external data sources without needing to retrain the model.",
			"HTMX allows you to access AJAX, CSS Transitions, WebSockets and Server Sent Events directly in HTML, using attributes, so you can build modern user interfaces with the simplicity and power of hypertext.",
			"Google Gemini is a family of multimodal large language models developed by Google DeepMind.",
		}

		log.Println("Seeding documents...")
		for i, doc := range seedDocs {
			vector, err := geminiSvc.GenerateEmbedding(ctx, doc)
			if err != nil {
				log.Fatalf("failed to embed seed doc: %v", err)
			}

			err = qdrantSvc.UpsertPoints(ctx, collectionName, []models.Point{
				{
					ID:     uint64(i + 1),
					Vector: vector,
					Payload: map[string]interface{}{
						"text": doc,
					},
				},
			})
			if err != nil {
				log.Fatalf("failed to upsert seed doc: %v", err)
			}
		}
		log.Println("Seeding complete.")
	}

	application := app.NewApp(qdrantSvc, geminiSvc)

	http.HandleFunc("/", application.HandleIndex)
	http.HandleFunc("/chat", application.HandleChat)
	http.HandleFunc("/chat/messages", application.HandleChatMessages)
	http.HandleFunc("/logs", application.HandleLogs)

	log.Println("Server starting on :8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
