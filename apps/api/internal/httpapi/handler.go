package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"full-stack-learning/apps/api/internal/biojob"
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

type BioJobManager interface {
	CreateOrGet(context.Context, biojob.CreateInput) (biojob.Job, error)
	Find(context.Context, string) (biojob.Job, error)
	Claim(context.Context, string, time.Duration) (biojob.ClaimResult, error)
	Complete(context.Context, string) (biojob.Job, error)
	RecordFailure(context.Context, string, string, bool) (biojob.Job, error)
}

type Handler struct {
	profiles ProfileFinder
	creator  ProfileCreator
	updater  ProfileUpdater
	auth     Authenticator
	database DatabasePinger
	jobs     BioJobManager
	jobKey   string
}

func (h *Handler) ConfigureBioJobs(jobs BioJobManager, internalKey string) {
	h.jobs = jobs
	h.jobKey = internalKey
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
	if h.jobs != nil {
		mux.HandleFunc("GET /api/bio-jobs/{jobId}", h.findBioJob)
		mux.HandleFunc("POST /internal/bio-jobs", h.createBioJob)
		mux.HandleFunc("POST /internal/bio-jobs/{jobId}/claim", h.claimBioJob)
		mux.HandleFunc("POST /internal/bio-jobs/{jobId}/complete", h.completeBioJob)
		mux.HandleFunc("POST /internal/bio-jobs/{jobId}/fail", h.failBioJob)
	}

	return mux
}

var jobIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type createBioJobRequest struct {
	JobID    string `json:"jobId"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

type failBioJobRequest struct {
	ErrorCode string `json:"errorCode"`
	Final     bool   `json:"final"`
}

func (h *Handler) createBioJob(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeInternal(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var request createBioJobRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if !jobIDPattern.MatchString(request.JobID) ||
		!usernamePattern.MatchString(request.Username) ||
		utf8.RuneCountInString(request.Name) < 1 ||
		utf8.RuneCountInString(request.Name) > 80 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid bio job"})
		return
	}

	job, err := h.jobs.CreateOrGet(r.Context(), biojob.CreateInput{
		JobID: request.JobID, Username: request.Username, Name: request.Name,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) findBioJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobId")
	if !jobIDPattern.MatchString(jobID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid job id"})
		return
	}
	job, err := h.jobs.Find(r.Context(), jobID)
	if errors.Is(err, biojob.ErrJobNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "bio job not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) claimBioJob(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeInternal(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	jobID := r.PathValue("jobId")
	if !jobIDPattern.MatchString(jobID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid job id"})
		return
	}
	result, err := h.jobs.Claim(r.Context(), jobID, 5*time.Minute)
	if errors.Is(err, biojob.ErrJobNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "bio job not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"claimed": result.Claimed, "job": result.Job})
}

func (h *Handler) completeBioJob(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeInternal(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	job, err := h.jobs.Complete(r.Context(), r.PathValue("jobId"))
	if errors.Is(err, biojob.ErrJobNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "bio job not found or profile missing"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) failBioJob(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeInternal(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var request failBioJobRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if !errorCodePattern.MatchString(request.ErrorCode) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid error code"})
		return
	}
	job, err := h.jobs.RecordFailure(r.Context(), r.PathValue("jobId"), request.ErrorCode, request.Final)
	if errors.Is(err, biojob.ErrJobNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "bio job not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) authorizeInternal(r *http.Request) bool {
	provided := r.Header.Get("X-Profile-Internal-Key")
	if h.jobKey == "" || len(provided) != len(h.jobKey) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(h.jobKey)) == 1
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return false
	}
	return true
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
