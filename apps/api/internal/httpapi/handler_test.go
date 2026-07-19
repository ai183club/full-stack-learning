package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"full-stack-learning/apps/api/internal/biojob"
	"full-stack-learning/apps/api/internal/profile"
)

type fakeProfileFinder struct {
	result  profile.Profile
	err     error
	authErr error
}

func (f fakeProfileFinder) FindByUsername(context.Context, string) (profile.Profile, error) {
	return f.result, f.err
}

func (f fakeProfileFinder) Create(context.Context, profile.CreateInput) (profile.Profile, error) {
	return f.result, f.err
}

func (f fakeProfileFinder) Update(context.Context, string, profile.UpdateInput) (profile.Profile, error) {
	return f.result, f.err
}

func (f fakeProfileFinder) Authenticate(context.Context, string, string) error {
	return f.authErr
}

type fakeDatabasePinger struct {
	err error
}

type fakeBioJobManager struct {
	job     biojob.Job
	claimed bool
	err     error
}

func (f fakeBioJobManager) CreateOrGet(context.Context, biojob.CreateInput) (biojob.Job, error) {
	return f.job, f.err
}

func (f fakeBioJobManager) Find(context.Context, string) (biojob.Job, error) {
	return f.job, f.err
}

func (f fakeBioJobManager) Claim(context.Context, string, time.Duration) (biojob.ClaimResult, error) {
	return biojob.ClaimResult{Job: f.job, Claimed: f.claimed}, f.err
}

func (f fakeBioJobManager) Complete(context.Context, string) (biojob.Job, error) {
	return f.job, f.err
}

func (f fakeBioJobManager) RecordFailure(context.Context, string, string, bool) (biojob.Job, error) {
	return f.job, f.err
}

func (f fakeDatabasePinger) Ping(context.Context) error {
	return f.err
}

func newAuthenticatedRequest(method string, target string, username string, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.SetBasicAuth(username, "learning-password")
	return request
}

func TestHealth(t *testing.T) {
	handler := NewHandler(fakeProfileFinder{}, fakeProfileFinder{}, fakeProfileFinder{}, fakeProfileFinder{}, fakeDatabasePinger{}).Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
}

func TestReadyWhenDatabaseIsUnavailable(t *testing.T) {
	handler := NewHandler(fakeProfileFinder{}, fakeProfileFinder{}, fakeProfileFinder{}, fakeProfileFinder{}, fakeDatabasePinger{err: errors.New("offline")}).Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, response.Code)
	}
}

func TestCreateAndFindBioJob(t *testing.T) {
	jobID := "e52f5f2d-c53c-4c2c-bb10-f378f82acc5d"
	bio := "Generated bio"
	jobs := fakeBioJobManager{job: biojob.Job{
		JobID: jobID, Name: "Alice", Status: biojob.StatusCompleted, Bio: &bio,
	}}
	handler := NewHandler(fakeProfileFinder{}, fakeProfileFinder{}, fakeProfileFinder{}, fakeProfileFinder{}, fakeDatabasePinger{})
	handler.ConfigureBioJobs(jobs, "internal-test-key")

	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/internal/bio-jobs",
		strings.NewReader(`{"jobId":"e52f5f2d-c53c-4c2c-bb10-f378f82acc5d","username":"bio_1234567890123456789012345678","name":"Alice"}`),
	)
	createRequest.Header.Set("X-Profile-Internal-Key", "internal-test-key")
	handler.Routes().ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d: %s", createResponse.Code, createResponse.Body.String())
	}

	findResponse := httptest.NewRecorder()
	handler.Routes().ServeHTTP(findResponse, httptest.NewRequest(http.MethodGet, "/api/bio-jobs/"+jobID, nil))
	if findResponse.Code != http.StatusOK || !strings.Contains(findResponse.Body.String(), `"bio":"Generated bio"`) {
		t.Fatalf("unexpected find response %d: %s", findResponse.Code, findResponse.Body.String())
	}
}

func TestInternalBioJobRoutesRequireKey(t *testing.T) {
	handler := NewHandler(fakeProfileFinder{}, fakeProfileFinder{}, fakeProfileFinder{}, fakeProfileFinder{}, fakeDatabasePinger{})
	handler.ConfigureBioJobs(fakeBioJobManager{}, "internal-test-key")
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/internal/bio-jobs", strings.NewReader(`{}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.Code)
	}
}

func TestFindProfile(t *testing.T) {
	finder := fakeProfileFinder{result: profile.Profile{
		ID:           1,
		Username:     "henry",
		PasswordHash: "must-not-be-returned",
		Name:         "Henry",
		Bio:          "Learning Go",
	}}
	handler := NewHandler(finder, finder, finder, finder, fakeDatabasePinger{}).Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/profiles/henry", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if strings.Contains(response.Body.String(), "must-not-be-returned") {
		t.Fatal("response exposed password hash")
	}
	if !strings.Contains(response.Body.String(), `"username":"henry"`) {
		t.Fatalf("response does not contain profile: %s", response.Body.String())
	}
}

func TestFindProfileNotFound(t *testing.T) {
	handler := NewHandler(fakeProfileFinder{err: pgx.ErrNoRows}, fakeProfileFinder{}, fakeProfileFinder{}, fakeProfileFinder{}, fakeDatabasePinger{}).Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/profiles/missing", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
}

func TestCreateProfile(t *testing.T) {
	creator := fakeProfileFinder{result: profile.Profile{ID: 2, Username: "alice", Name: "Alice"}}
	handler := NewHandler(creator, creator, creator, creator, fakeDatabasePinger{}).Routes()
	requestBody := `{"username":"alice","password":"password123","name":"Alice","bio":"Learning Go"}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(requestBody)))

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "password") {
		t.Fatalf("response exposed password data: %s", response.Body.String())
	}
}

func TestCreateProfileValidation(t *testing.T) {
	creator := fakeProfileFinder{}
	handler := NewHandler(creator, creator, creator, creator, fakeDatabasePinger{}).Routes()
	requestBody := `{"username":"A","password":"short","name":"","bio":""}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(requestBody)))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
	for _, field := range []string{"username", "password", "name"} {
		if !strings.Contains(response.Body.String(), field) {
			t.Fatalf("response does not contain %q validation error: %s", field, response.Body.String())
		}
	}
}

func TestCreateProfileBioCharacterLimit(t *testing.T) {
	tests := []struct {
		name       string
		bio        string
		wantStatus int
	}{
		{name: "empty", bio: "", wantStatus: http.StatusCreated},
		{name: "500 Unicode characters", bio: strings.Repeat("界", profile.MaxBioCharacters), wantStatus: http.StatusCreated},
		{name: "501 Unicode characters", bio: strings.Repeat("界", profile.MaxBioCharacters+1), wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			creator := fakeProfileFinder{result: profile.Profile{ID: 2, Username: "alice", Name: "Alice"}}
			handler := NewHandler(creator, creator, creator, creator, fakeDatabasePinger{}).Routes()
			requestBody := fmt.Sprintf(
				`{"username":"alice","password":"password123","name":"Alice","bio":"%s"}`,
				test.bio,
			)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(requestBody)))

			if response.Code != test.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", test.wantStatus, response.Code, response.Body.String())
			}
			if test.wantStatus == http.StatusBadRequest && !strings.Contains(response.Body.String(), `"bio"`) {
				t.Fatalf("response does not contain bio validation error: %s", response.Body.String())
			}
		})
	}
}

func TestCreateProfileConflict(t *testing.T) {
	creator := fakeProfileFinder{err: profile.ErrUsernameTaken}
	handler := NewHandler(creator, creator, creator, creator, fakeDatabasePinger{}).Routes()
	requestBody := `{"username":"alice","password":"password123","name":"Alice","bio":""}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(requestBody)))

	if response.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, response.Code)
	}
}

func TestUpdateProfile(t *testing.T) {
	updater := fakeProfileFinder{result: profile.Profile{
		ID:       2,
		Username: "alice",
		Name:     "Alice Chen",
		Bio:      "Updated bio",
	}}
	handler := NewHandler(updater, updater, updater, updater, fakeDatabasePinger{}).Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, newAuthenticatedRequest(
		http.MethodPatch,
		"/api/profiles/alice",
		"alice",
		`{"name":"Alice Chen","bio":"Updated bio"}`,
	))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"name":"Alice Chen"`) {
		t.Fatalf("response does not contain updated profile: %s", response.Body.String())
	}
}

func TestUpdateProfileRequiresAField(t *testing.T) {
	updater := fakeProfileFinder{}
	handler := NewHandler(updater, updater, updater, updater, fakeDatabasePinger{}).Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, newAuthenticatedRequest(
		http.MethodPatch,
		"/api/profiles/alice",
		"alice",
		`{}`,
	))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func TestUpdateProfileBioCharacterLimit(t *testing.T) {
	tests := []struct {
		name       string
		bio        string
		wantStatus int
	}{
		{name: "empty", bio: "", wantStatus: http.StatusOK},
		{name: "500 Unicode characters", bio: strings.Repeat("界", profile.MaxBioCharacters), wantStatus: http.StatusOK},
		{name: "501 Unicode characters", bio: strings.Repeat("界", profile.MaxBioCharacters+1), wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updater := fakeProfileFinder{result: profile.Profile{ID: 2, Username: "alice", Name: "Alice", Bio: test.bio}}
			handler := NewHandler(updater, updater, updater, updater, fakeDatabasePinger{}).Routes()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, newAuthenticatedRequest(
				http.MethodPatch,
				"/api/profiles/alice",
				"alice",
				fmt.Sprintf(`{"bio":"%s"}`, test.bio),
			))

			if response.Code != test.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", test.wantStatus, response.Code, response.Body.String())
			}
		})
	}
}

func TestUpdateProfileNotFound(t *testing.T) {
	updater := fakeProfileFinder{err: profile.ErrProfileNotFound}
	handler := NewHandler(updater, updater, updater, updater, fakeDatabasePinger{}).Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, newAuthenticatedRequest(
		http.MethodPatch,
		"/api/profiles/missing",
		"missing",
		`{"bio":"new bio"}`,
	))

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
}

func TestUpdateProfileRequiresAuthentication(t *testing.T) {
	updater := fakeProfileFinder{}
	handler := NewHandler(updater, updater, updater, updater, fakeDatabasePinger{}).Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPatch,
		"/api/profiles/alice",
		strings.NewReader(`{"name":"Alice Chen"}`),
	))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
	if response.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("response does not contain WWW-Authenticate header")
	}
}

func TestUpdateProfileRejectsInvalidCredentials(t *testing.T) {
	updater := fakeProfileFinder{authErr: profile.ErrInvalidCredentials}
	handler := NewHandler(updater, updater, updater, updater, fakeDatabasePinger{}).Routes()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, newAuthenticatedRequest(
		http.MethodPatch,
		"/api/profiles/alice",
		"alice",
		`{"name":"Alice Chen"}`,
	))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}
