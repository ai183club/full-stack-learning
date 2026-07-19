package biojob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateOrGet(ctx context.Context, input CreateInput) (Job, error) {
	const query = `
		INSERT INTO bio_generation_jobs (job_id, username, name, status)
		VALUES (
			$1::varchar(36),
			$2::varchar(32),
			$3::varchar(80),
			CASE WHEN EXISTS (
				SELECT 1 FROM profiles WHERE username = $2::varchar(32)
			)
				THEN 'completed' ELSE 'pending' END
		)
		ON CONFLICT (username) DO UPDATE SET
			name = EXCLUDED.name,
			status = CASE
				WHEN EXISTS (SELECT 1 FROM profiles WHERE username = EXCLUDED.username)
					THEN 'completed'
				WHEN bio_generation_jobs.status = 'failed' THEN 'pending'
				ELSE bio_generation_jobs.status
			END,
			error_code = CASE
				WHEN bio_generation_jobs.status = 'failed' THEN NULL
				ELSE bio_generation_jobs.error_code
			END,
			updated_at = NOW()
		RETURNING job_id, username, name, status, error_code,
			attempt_count, lease_expires_at, created_at, updated_at
	`

	job, err := scanJobWithoutBio(r.pool.QueryRow(ctx, query, input.JobID, input.Username, input.Name))
	if err != nil {
		return Job{}, fmt.Errorf("create or get bio job: %w", err)
	}
	return job, nil
}

func (r *Repository) Find(ctx context.Context, jobID string) (Job, error) {
	const query = `
		SELECT jobs.job_id, jobs.username, jobs.name, jobs.status, jobs.error_code,
			profiles.bio, jobs.attempt_count, jobs.lease_expires_at,
			jobs.created_at, jobs.updated_at
		FROM bio_generation_jobs AS jobs
		LEFT JOIN profiles ON profiles.username = jobs.username
		WHERE jobs.job_id = $1
	`

	var job Job
	err := r.pool.QueryRow(ctx, query, jobID).Scan(
		&job.JobID,
		&job.Username,
		&job.Name,
		&job.Status,
		&job.ErrorCode,
		&job.Bio,
		&job.AttemptCount,
		&job.LeaseExpires,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("find bio job: %w", err)
	}
	if job.Status != StatusCompleted {
		job.Bio = nil
	}
	return job, nil
}

func (r *Repository) Claim(ctx context.Context, jobID string, leaseDuration time.Duration) (ClaimResult, error) {
	const query = `
		UPDATE bio_generation_jobs
		SET status = 'running',
			attempt_count = attempt_count + 1,
			error_code = NULL,
			lease_expires_at = NOW() + ($2 * INTERVAL '1 millisecond'),
			updated_at = NOW()
		WHERE job_id = $1
			AND (
				status IN ('pending', 'failed')
				OR (status = 'running' AND lease_expires_at <= NOW())
			)
		RETURNING job_id, username, name, status, error_code,
			attempt_count, lease_expires_at, created_at, updated_at
	`

	job, err := scanJobWithoutBio(r.pool.QueryRow(ctx, query, jobID, leaseDuration.Milliseconds()))
	if err == nil {
		return ClaimResult{Job: job, Claimed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ClaimResult{}, fmt.Errorf("claim bio job: %w", err)
	}

	job, err = r.Find(ctx, jobID)
	if err != nil {
		return ClaimResult{}, err
	}
	return ClaimResult{Job: job, Claimed: false}, nil
}

func (r *Repository) Complete(ctx context.Context, jobID string) (Job, error) {
	const query = `
		UPDATE bio_generation_jobs AS jobs
		SET status = 'completed', error_code = NULL, lease_expires_at = NULL, updated_at = NOW()
		WHERE jobs.job_id = $1
			AND EXISTS (SELECT 1 FROM profiles WHERE username = jobs.username)
		RETURNING job_id, username, name, status, error_code,
			attempt_count, lease_expires_at, created_at, updated_at
	`

	job, err := scanJobWithoutBio(r.pool.QueryRow(ctx, query, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("complete bio job: %w", err)
	}
	return job, nil
}

func (r *Repository) RecordFailure(ctx context.Context, jobID string, errorCode string, final bool) (Job, error) {
	status := StatusPending
	if final {
		status = StatusFailed
	}

	const query = `
		UPDATE bio_generation_jobs
		SET status = $2, error_code = $3, lease_expires_at = NULL, updated_at = NOW()
		WHERE job_id = $1 AND status <> 'completed'
		RETURNING job_id, username, name, status, error_code,
			attempt_count, lease_expires_at, created_at, updated_at
	`

	job, err := scanJobWithoutBio(r.pool.QueryRow(ctx, query, jobID, status, errorCode))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("record bio job failure: %w", err)
	}
	return job, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJobWithoutBio(row rowScanner) (Job, error) {
	var job Job
	err := row.Scan(
		&job.JobID,
		&job.Username,
		&job.Name,
		&job.Status,
		&job.ErrorCode,
		&job.AttemptCount,
		&job.LeaseExpires,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	return job, err
}
