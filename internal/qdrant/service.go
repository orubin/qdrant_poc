package qdrant

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
	"qdrant-poc/pkg/models"
)

type Service struct {
	client *qdrant.Client
}

func NewService(host string, port int) (*Service, error) {
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: host,
		Port: port,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create qdrant client: %w", err)
	}

	return &Service{client: client}, nil
}

func (s *Service) Close() error {
	return s.client.Close()
}

func (s *Service) CreateCollection(ctx context.Context, name string, size uint64) error {
	err := s.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: name,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     size,
			Distance: qdrant.Distance_Cosine,
		}),
	})
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}
	return nil
}

func (s *Service) CollectionExists(ctx context.Context, name string) (bool, error) {
	exists, err := s.client.CollectionExists(ctx, name)
	if err != nil {
		return false, fmt.Errorf("failed to check collection existence: %w", err)
	}
	return exists, nil
}

func (s *Service) GetCollectionStatus(ctx context.Context, name string) (uint64, error) {
	info, err := s.client.GetCollectionInfo(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("failed to get collection info: %w", err)
	}
	return info.PointsCount, nil
}

func (s *Service) UpsertPoints(ctx context.Context, collectionName string, points []models.Point) error {
	qPoints := make([]*qdrant.PointStruct, len(points))
	for i, p := range points {
		qPoints[i] = &qdrant.PointStruct{
			Id:      qdrant.NewIDNum(p.ID),
			Vectors: qdrant.NewVectors(p.Vector...),
			Payload: qdrant.NewValueMap(p.Payload),
		}
	}

	_, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collectionName,
		Points:         qPoints,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert points: %w", err)
	}
	return nil
}

func (s *Service) Search(ctx context.Context, collectionName string, vector []float32, limit uint64) ([]models.SearchResult, error) {
	res, err := s.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: collectionName,
		Query:          qdrant.NewQuery(vector...),
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}

	results := make([]models.SearchResult, len(res))
	for i, hit := range res {
		results[i] = models.SearchResult{
			ID:      hit.Id.GetNum(),
			Score:   hit.Score,
			Payload: hit.Payload,
		}
	}

	return results, nil
}
