package gemini

import (
	"context"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type Service struct {
	client *genai.Client
	model  *genai.GenerativeModel
	embed  *genai.EmbeddingModel
}

func NewService(ctx context.Context, apiKey string) (*Service, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

	model := client.GenerativeModel("gemini-1.5-flash")
	embed := client.EmbeddingModel("embedding-001")

	return &Service{
		client: client,
		model:  model,
		embed:  embed,
	}, nil
}

func (s *Service) Close() error {
	return s.client.Close()
}

func (s *Service) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	res, err := s.embed.EmbedContent(ctx, genai.Text(text))
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding: %w", err)
	}

	return res.Embedding.Values, nil
}

func (s *Service) GenerateResponse(ctx context.Context, prompt string, contextItems []string) (string, error) {
	fullPrompt := "You are a helpful assistant. Use the following context to answer the question.\n\n"
	fullPrompt += "Context:\n"
	for _, item := range contextItems {
		fullPrompt += fmt.Sprintf("- %s\n", item)
	}
	fullPrompt += fmt.Sprintf("\nQuestion: %s\nAnswer:", prompt)

	resp, err := s.model.GenerateContent(ctx, genai.Text(fullPrompt))
	if err != nil {
		return "", fmt.Errorf("failed to generate response: %w", err)
	}

	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no candidates in response")
	}

	part := resp.Candidates[0].Content.Parts[0]
	if text, ok := part.(genai.Text); ok {
		return string(text), nil
	}

	return "", fmt.Errorf("response part is not text")
}
