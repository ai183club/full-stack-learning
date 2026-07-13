package profile

import (
	"errors"
	"time"
	"unicode/utf8"
)

var ErrUsernameTaken = errors.New("username is already taken")
var ErrProfileNotFound = errors.New("profile not found")
var ErrInvalidCredentials = errors.New("invalid username or password")
var ErrBioTooLong = errors.New("bio must be at most 500 characters")

const MaxBioCharacters = 500

func ValidateBio(bio string) error {
	if utf8.RuneCountInString(bio) > MaxBioCharacters {
		return ErrBioTooLong
	}

	return nil
}

type Profile struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Name         string    `json:"name"`
	Bio          string    `json:"bio"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type CreateInput struct {
	Username string
	Password string
	Name     string
	Bio      string
}

type CreateParams struct {
	Username     string
	PasswordHash string
	Name         string
	Bio          string
}

type UpdateInput struct {
	Name *string
	Bio  *string
}
