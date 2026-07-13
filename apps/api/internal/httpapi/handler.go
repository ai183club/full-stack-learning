package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"full-stack-learning/apps/api/internal/profile"
)

type ProfileFinder interface {
	FindByUsername(ctx context.Context, username string) (profile.Profile, error)
}

type ProfileCreator interface {
	Create(ctx context.Context, input profile.CreateInput) (profile.Profile, error)
}

type ProfileUpdater interface {
	Update(ctx context.Context, username string, input profile.UpdateInput) (profile.Profile, error)
}

type Authenticator interface {
	Authenticate(ctx context.Context, username string, password string) error
}

type DatabasePinger interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	profiles ProfileFinder
	creator  ProfileCreator
	updater  ProfileUpdater
	auth     Authenticator
	database DatabasePinger
}

func NewHandler(
	profiles ProfileFinder,
	creator ProfileCreator,
	updater ProfileUpdater,
	auth Authenticator,
	database DatabasePinger,
) *Handler {
	return &Handler{profiles: profiles, creator: creator, updater: updater, auth: auth, database: database}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /ready", h.ready)
	mux.HandleFunc("POST /api/profiles", h.createProfile)
	mux.HandleFunc("GET /api/profiles/{username}", h.findProfile)
	mux.HandleFunc("PATCH /api/profiles/{username}", h.updateProfile)

	return mux
}

type updateProfileRequest struct {
	Name *string `json:"name"`
	Bio  *string `json:"bio"`
}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	authUsername, password, ok := r.BasicAuth()
	if !ok || authUsername != username {
		writeUnauthorized(w)
		return
	}
	if err := h.auth.Authenticate(r.Context(), authUsername, password); err != nil {
		if errors.Is(err, profile.ErrInvalidCredentials) {
			writeUnauthorized(w)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var request updateProfileRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return
	}
	if request.Name == nil && request.Bio == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name or bio is required"})
		return
	}
	if request.Name != nil {
		trimmedName := strings.TrimSpace(*request.Name)
		request.Name = &trimmedName
		if length := utf8.RuneCountInString(trimmedName); length < 1 || length > 80 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name must be 1-80 characters"})
			return
		}
	}
	if request.Bio != nil {
		trimmedBio := strings.TrimSpace(*request.Bio)
		request.Bio = &trimmedBio
		if err := profile.ValidateBio(trimmedBio); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	result, err := h.updater.Update(r.Context(), username, profile.UpdateInput{
		Name: request.Name,
		Bio:  request.Bio,
	})
	if errors.Is(err, profile.ErrProfileNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found"})
		return
	}
	if errors.Is(err, profile.ErrBioTooLong) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="profile-api", charset="UTF-8"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9_]{3,32}$`)

type createProfileRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Bio      string `json:"bio"`
}

func (h *Handler) createProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var request createProfileRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return
	}

	request.Username = strings.TrimSpace(request.Username)
	request.Name = strings.TrimSpace(request.Name)
	request.Bio = strings.TrimSpace(request.Bio)
	if validationErrors := validateCreateProfile(request); len(validationErrors) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "validation failed",
			"fields": validationErrors,
		})
		return
	}

	result, err := h.creator.Create(r.Context(), profile.CreateInput{
		Username: request.Username,
		Password: request.Password,
		Name:     request.Name,
		Bio:      request.Bio,
	})
	if errors.Is(err, profile.ErrUsernameTaken) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username is already taken"})
		return
	}
	if errors.Is(err, profile.ErrBioTooLong) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "validation failed",
			"fields": map[string]string{
				"bio": "must be at most 500 characters",
			},
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

func validateCreateProfile(request createProfileRequest) map[string]string {
	validationErrors := make(map[string]string)
	if !usernamePattern.MatchString(request.Username) {
		validationErrors["username"] = "must be 3-32 lowercase letters, numbers, or underscores"
	}
	if len(request.Password) < 8 || len(request.Password) > 72 {
		validationErrors["password"] = "must be 8-72 bytes"
	}
	if length := utf8.RuneCountInString(request.Name); length < 1 || length > 80 {
		validationErrors["name"] = "must be 1-80 characters"
	}
	if err := profile.ValidateBio(request.Bio); err != nil {
		validationErrors["bio"] = "must be at most 500 characters"
	}
	return validationErrors
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.database.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
			"error":  "database is not ready",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) findProfile(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required"})
		return
	}

	result, err := h.profiles.FindByUsername(r.Context(), username)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
