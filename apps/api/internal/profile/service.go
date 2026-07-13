package profile

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type ProfileRepository interface {
	FindByUsername(ctx context.Context, username string) (Profile, error)
	Create(ctx context.Context, params CreateParams) (Profile, error)
	Update(ctx context.Context, username string, input UpdateInput) (Profile, error)
}

func (s *Service) Authenticate(ctx context.Context, username string, password string) error {
	result, err := s.repository.FindByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidCredentials
	}
	if err != nil {
		return fmt.Errorf("find profile for authentication: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(result.PasswordHash), []byte(password)); err != nil {
		return ErrInvalidCredentials
	}

	return nil
}

func (s *Service) Update(ctx context.Context, username string, input UpdateInput) (Profile, error) {
	if input.Bio != nil {
		if err := ValidateBio(*input.Bio); err != nil {
			return Profile{}, err
		}
	}

	return s.repository.Update(ctx, username, input)
}

type Service struct {
	repository ProfileRepository
}

func NewService(repository ProfileRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) FindByUsername(ctx context.Context, username string) (Profile, error) {
	return s.repository.FindByUsername(ctx, username)
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Profile, error) {
	if err := ValidateBio(input.Bio); err != nil {
		return Profile{}, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return Profile{}, fmt.Errorf("hash password: %w", err)
	}

	return s.repository.Create(ctx, CreateParams{
		Username:     input.Username,
		PasswordHash: string(passwordHash),
		Name:         input.Name,
		Bio:          input.Bio,
	})
}
