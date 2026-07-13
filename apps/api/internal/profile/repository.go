package profile

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func (r *Repository) Create(ctx context.Context, params CreateParams) (Profile, error) {
	const query = `
		INSERT INTO profiles (username, password_hash, name, bio)
		VALUES ($1, $2, $3, $4)
		RETURNING id, username, password_hash, name, bio, created_at, updated_at
	`

	var result Profile
	err := r.pool.QueryRow(
		ctx,
		query,
		params.Username,
		params.PasswordHash,
		params.Name,
		params.Bio,
	).Scan(
		&result.ID,
		&result.Username,
		&result.PasswordHash,
		&result.Name,
		&result.Bio,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return Profile{}, ErrUsernameTaken
		}

		return Profile{}, fmt.Errorf("create profile: %w", err)
	}

	return result, nil
}

func (r *Repository) Update(ctx context.Context, username string, input UpdateInput) (Profile, error) {
	const query = `
		UPDATE profiles
		SET
			name = COALESCE($2, name),
			bio = COALESCE($3, bio),
			updated_at = NOW()
		WHERE username = $1
		RETURNING id, username, password_hash, name, bio, created_at, updated_at
	`

	var result Profile
	err := r.pool.QueryRow(ctx, query, username, input.Name, input.Bio).Scan(
		&result.ID,
		&result.Username,
		&result.PasswordHash,
		&result.Name,
		&result.Bio,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrProfileNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("update profile: %w", err)
	}

	return result, nil
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) FindByUsername(ctx context.Context, username string) (Profile, error) {
	const query = `
		SELECT id, username, password_hash, name, bio, created_at, updated_at
		FROM profiles
		WHERE username = $1
	`

	var result Profile
	err := r.pool.QueryRow(ctx, query, username).Scan(
		&result.ID,
		&result.Username,
		&result.PasswordHash,
		&result.Name,
		&result.Bio,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return Profile{}, fmt.Errorf("find profile by username: %w", err)
	}

	return result, nil
}
