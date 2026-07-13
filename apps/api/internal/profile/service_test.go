package profile

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type fakeRepository struct {
	created     CreateParams
	updated     UpdateInput
	createCalls int
	updateCalls int
	found       Profile
	findErr     error
}

func (f *fakeRepository) FindByUsername(context.Context, string) (Profile, error) {
	return f.found, f.findErr
}

func TestAuthenticate(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repository := &fakeRepository{found: Profile{Username: "alice", PasswordHash: string(passwordHash)}}
	service := NewService(repository)

	if err := service.Authenticate(context.Background(), "alice", "correct-password"); err != nil {
		t.Fatalf("authenticate correct password: %v", err)
	}
	if err := service.Authenticate(context.Background(), "alice", "wrong-password"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func (f *fakeRepository) Create(_ context.Context, params CreateParams) (Profile, error) {
	f.createCalls++
	f.created = params
	return Profile{Username: params.Username, PasswordHash: params.PasswordHash}, nil
}

func (f *fakeRepository) Update(_ context.Context, _ string, input UpdateInput) (Profile, error) {
	f.updateCalls++
	f.updated = input
	return Profile{}, nil
}

func TestCreateHashesPassword(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository)
	plainPassword := "learning-password"

	_, err := service.Create(context.Background(), CreateInput{
		Username: "alice",
		Password: plainPassword,
		Name:     "Alice",
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if repository.created.PasswordHash == plainPassword {
		t.Fatal("repository received the plain-text password")
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(repository.created.PasswordHash),
		[]byte(plainPassword),
	); err != nil {
		t.Fatalf("stored hash does not match password: %v", err)
	}
}

func TestValidateBioCountsUnicodeCharacters(t *testing.T) {
	tests := []struct {
		name    string
		bio     string
		wantErr bool
	}{
		{name: "empty", bio: ""},
		{name: "exactly 500 Unicode characters", bio: strings.Repeat("界", MaxBioCharacters)},
		{name: "501 Unicode characters", bio: strings.Repeat("界", MaxBioCharacters+1), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateBio(test.bio)
			if test.wantErr && !errors.Is(err, ErrBioTooLong) {
				t.Fatalf("expected ErrBioTooLong, got %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("expected valid bio, got %v", err)
			}
		})
	}
}

func TestCreateEnforcesBioCharacterLimit(t *testing.T) {
	validRepository := &fakeRepository{}
	validService := NewService(validRepository)
	_, err := validService.Create(context.Background(), CreateInput{
		Username: "alice",
		Password: "learning-password",
		Name:     "Alice",
		Bio:      strings.Repeat("界", MaxBioCharacters),
	})
	if err != nil {
		t.Fatalf("create profile with 500-character bio: %v", err)
	}
	if validRepository.createCalls != 1 {
		t.Fatalf("expected one repository create call, got %d", validRepository.createCalls)
	}

	invalidRepository := &fakeRepository{}
	invalidService := NewService(invalidRepository)
	_, err = invalidService.Create(context.Background(), CreateInput{
		Username: "alice",
		Password: "learning-password",
		Name:     "Alice",
		Bio:      strings.Repeat("界", MaxBioCharacters+1),
	})
	if !errors.Is(err, ErrBioTooLong) {
		t.Fatalf("expected ErrBioTooLong, got %v", err)
	}
	if invalidRepository.createCalls != 0 {
		t.Fatal("repository create was called for an invalid bio")
	}
}

func TestUpdateEnforcesBioCharacterLimit(t *testing.T) {
	emptyBio := ""
	validBio := strings.Repeat("界", MaxBioCharacters)
	invalidBio := strings.Repeat("界", MaxBioCharacters+1)

	tests := []struct {
		name    string
		bio     *string
		wantErr bool
	}{
		{name: "omitted bio", bio: nil},
		{name: "empty bio", bio: &emptyBio},
		{name: "exactly 500 Unicode characters", bio: &validBio},
		{name: "501 Unicode characters", bio: &invalidBio, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{}
			service := NewService(repository)

			_, err := service.Update(context.Background(), "alice", UpdateInput{Bio: test.bio})
			if test.wantErr {
				if !errors.Is(err, ErrBioTooLong) {
					t.Fatalf("expected ErrBioTooLong, got %v", err)
				}
				if repository.updateCalls != 0 {
					t.Fatal("repository update was called for an invalid bio")
				}
				return
			}

			if err != nil {
				t.Fatalf("update profile: %v", err)
			}
			if repository.updateCalls != 1 {
				t.Fatalf("expected one repository update call, got %d", repository.updateCalls)
			}
		})
	}
}
