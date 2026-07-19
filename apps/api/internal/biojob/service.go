package biojob

import (
	"context"
	"time"
)

type JobRepository interface {
	CreateOrGet(context.Context, CreateInput) (Job, error)
	Find(context.Context, string) (Job, error)
	Claim(context.Context, string, time.Duration) (ClaimResult, error)
	Complete(context.Context, string) (Job, error)
	RecordFailure(context.Context, string, string, bool) (Job, error)
}

type Service struct {
	repository JobRepository
}

func NewService(repository JobRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) CreateOrGet(ctx context.Context, input CreateInput) (Job, error) {
	return s.repository.CreateOrGet(ctx, input)
}

func (s *Service) Find(ctx context.Context, jobID string) (Job, error) {
	return s.repository.Find(ctx, jobID)
}

func (s *Service) Claim(ctx context.Context, jobID string, lease time.Duration) (ClaimResult, error) {
	return s.repository.Claim(ctx, jobID, lease)
}

func (s *Service) Complete(ctx context.Context, jobID string) (Job, error) {
	return s.repository.Complete(ctx, jobID)
}

func (s *Service) RecordFailure(ctx context.Context, jobID string, errorCode string, final bool) (Job, error) {
	return s.repository.RecordFailure(ctx, jobID, errorCode, final)
}
