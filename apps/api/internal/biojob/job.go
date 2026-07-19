package biojob

import (
	"errors"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

var ErrJobNotFound = errors.New("bio generation job not found")

type Job struct {
	JobID        string     `json:"jobId"`
	Username     string     `json:"-"`
	Name         string     `json:"name"`
	Status       Status     `json:"status"`
	ErrorCode    *string    `json:"errorCode,omitempty"`
	Bio          *string    `json:"bio,omitempty"`
	AttemptCount int        `json:"-"`
	LeaseExpires *time.Time `json:"-"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type CreateInput struct {
	JobID    string
	Username string
	Name     string
}

type ClaimResult struct {
	Job     Job
	Claimed bool
}
