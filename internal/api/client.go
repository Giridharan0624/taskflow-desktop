package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"

	"taskflow-desktop/internal/auth"
	"taskflow-desktop/internal/config"
	"taskflow-desktop/internal/queue"
	"taskflow-desktop/internal/security"
	"taskflow-desktop/internal/state"
)

// allowedUploadHosts is the host allowlist for presigned S3 upload URLs
// returned by the backend. A compromised or misconfigured backend could
// otherwise redirect screenshot PUTs (which contain a frame of the
// user's screen) to an attacker-controlled host. See C-API-1.
//
// Covers:
//   - amazonaws.com: any AWS-hosted S3 endpoint (s3.ap-south-1.amazonaws.com,
//     taskflow-uploads.s3.amazonaws.com, s3.amazonaws.com, etc.)
//   - cloudfront.net: CloudFront distributions in front of S3
//
// If you introduce a custom domain for uploads, add it here.
var allowedUploadHosts = []string{
	"amazonaws.com",
	"cloudfront.net",
}

// StartTimerData is the payload for POST /attendance/sign-in.
type StartTimerData struct {
	TaskID      string `json:"taskId"`
	ProjectID   string `json:"projectId"`
	TaskTitle   string `json:"taskTitle"`
	ProjectName string `json:"projectName"`
	Description string `json:"description"`
}

// Attendance mirrors the backend attendance response (camelCase for frontend).
type Attendance = state.Attendance

// CurrentTask mirrors the backend current task.
type CurrentTask = state.CurrentTask

// Task represents a user's assigned task.
type Task struct {
	TaskID      string   `json:"taskId"`
	ProjectID   string   `json:"projectId"`
	Title       string   `json:"title"`
	Description *string  `json:"description"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority"`
	Domain      string   `json:"domain"`
	AssignedTo  []string `json:"assignedTo"`
	Deadline    string   `json:"deadline"`
	ProjectName string   `json:"projectName,omitempty"`
}

// User represents the current user profile.
type User struct {
	UserID     string   `json:"userId"`
	Email      string   `json:"email"`
	Name       string   `json:"name"`
	SystemRole string   `json:"systemRole"`
	Department *string  `json:"department"`
	AvatarURL  *string  `json:"avatarUrl"`
	EmployeeID *string  `json:"employeeId"`
	Skills     []string `json:"skills"`
}

// OrgSettings is the desktop-relevant subset of the backend's
// /orgs/current/settings response. The desktop only cares about feature
// toggles (specifically `screenshots`) and the display name shown in the
// tray tooltip; everything else is ignored.
type OrgSettings struct {
	DisplayName string          `json:"displayName"`
	Features    map[string]bool `json:"features"`
}

// Client handles all API communication with the backend.
//
// IMPORTANT: Treat c.http and c.uploads as immutable after NewClient.
// Do NOT call SetHeader / SetBody / SetAuthToken etc. on the client
// itself — those mutations are not synchronised and would cross
// between concurrent callers' requests (token of caller A could land
// on caller B's request). Always use c.http.R() which returns a fresh
// *resty.Request that is request-local. See V3-M10.
type Client struct {
	http        *resty.Client
	// uploads is a separate resty.Client with a longer timeout for
	// S3 PUT uploads. Previously all requests shared one 30s budget,
	// so a 2 MB screenshot over a slow uplink would silently time out
	// and the heartbeat would land without the screenshot URL. See
	// M-API-2.
	uploads     *resty.Client
	authService *auth.Service
	appState    *state.AppState

	// settingsMu guards both fields below — the cache is read by the
	// activity loop on every screenshot tick and written by RefreshSettings
	// (called after login + periodically). Snapshot semantics: callers
	// receive a copy, never a live pointer.
	settingsMu sync.RWMutex
	settings   *OrgSettings

	// tasksCache persists the last successful /users/me/tasks
	// response so the TaskSelector can render something while
	// offline. Best-effort: nil if the on-disk cache couldn't be
	// created. See V3-offline.
	tasksCache *queue.TasksCache
}

// NewClient creates a new API client with HTTPS enforced and TLS 1.3 minimum.
func NewClient(authService *auth.Service, appState *state.AppState) *Client {
	cfg := config.Get()

	// Enforce HTTPS — reject HTTP URLs
	if !strings.HasPrefix(cfg.APIURL, "https://") {
		panic("API URL must use HTTPS — refusing to start with insecure connection")
	}

	// TLS 1.3 minimum, no insecure ciphers. Shared across both resty
	// clients — cert caching and connection pooling stay unified.
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	}

	// Redirect policy shared by both clients: HTTPS-only, max 10 hops.
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-https redirect to %q", req.URL.String())
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}

	client := resty.NewWithClient(&http.Client{
		Transport:     transport,
		Timeout:       30 * time.Second,
		CheckRedirect: checkRedirect,
	}).
		SetBaseURL(cfg.APIURL).
		SetHeader("Content-Type", "application/json").
		// Retry on transient network errors (Wi-Fi drop, DNS blip).
		// One extra attempt with a 2-second backoff turns a 5-second
		// outage from "lost heartbeat" into "delayed heartbeat". We
		// don't retry on 4xx/5xx because those can be non-idempotent
		// (a heartbeat already applied on the server, for instance)
		// — only transport-level failures get a retry. See V3-M4.
		SetRetryCount(1).
		SetRetryWaitTime(2 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second).
		AddRetryCondition(func(r *resty.Response, err error) bool {
			// Retry only on transport-level failures (err != nil
			// and no HTTP response). Any HTTP status code, even
			// 5xx, means the request was delivered and may have
			// been processed.
			return err != nil && r == nil
		})

	// Uploads get 3 minutes. A multi-MB screenshot over a hotel-WiFi
	// uplink can easily take >30s; without a separate budget we
	// silently drop screenshots during bad network windows.
	uploads := resty.NewWithClient(&http.Client{
		Transport:     transport,
		Timeout:       3 * time.Minute,
		CheckRedirect: checkRedirect,
	})

	c := &Client{
		http:        client,
		uploads:     uploads,
		authService: authService,
		appState:    appState,
	}
	if tc, err := queue.NewTasksCache(); err != nil {
		log.Printf("api: tasks cache disabled (%v) — offline launch will show empty list", err)
	} else {
		c.tasksCache = tc
	}
	return c
}

// request creates an authenticated request with auto-refreshed JWT.
//
// The backend authorizes on the JWT's `custom:orgId` claim alone —
// there's no per-request workspace header anymore. Option-B login (email
// + password on a single hostname) resolves the org from Cognito, not
// from a client-supplied slug.
func (c *Client) request() (*resty.Request, error) {
	token, err := c.authService.GetIDToken()
	if err != nil {
		// Wrap with the sentinel so callers can errors.Is() and
		// distinguish "local auth state is gone" from "backend said
		// 401". The heartbeat goroutine uses this to stop the retry
		// loop after logout. See V3-M5.
		return nil, fmt.Errorf("%w: %v", ErrNotAuthenticated, err)
	}

	return c.http.R().SetHeader("Authorization", "Bearer "+token), nil
}

// GetOrgSettings fetches the current org's settings and refreshes the
// in-memory cache.
//
// Note: we call `GET /orgs/current` (which returns org + settings +
// plan + pipelines) rather than `GET /orgs/current/settings` because
// the latter route was never deployed — only PUT is wired up there.
// Calling the nonexistent GET produces a 403 IAM-signature error from
// API Gateway's default fallthrough handler. The parent endpoint
// gives us what we need as a single hydration call anyway; we just
// pluck out the settings field. See V3-client.
//
// On any error the previous cached value is preserved — a transient
// network blip never flips the feature gate. Callers that need the
// last-known good cache without a network round-trip use
// CachedSettings() instead.
func (c *Client) GetOrgSettings() (*OrgSettings, error) {
	req, err := c.request()
	if err != nil {
		return nil, err
	}
	resp, err := req.Get("/orgs/current")
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, apiError("fetch settings", resp)
	}
	converted, err := snakeToCamel(resp.Body())
	if err != nil {
		return nil, fmt.Errorf("settings response: %w", err)
	}
	// GET /orgs/current returns {org, settings, plan, pipelines}.
	// Extract just the settings field; tolerate null (a brand-new
	// org that hasn't been set up yet) by returning an empty
	// OrgSettings so fail-closed feature gates (e.g. screenshots)
	// still behave correctly.
	var envelope struct {
		Settings *OrgSettings `json:"settings"`
	}
	if err := json.Unmarshal(converted, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse settings envelope: %w", err)
	}
	settings := envelope.Settings
	if settings == nil {
		settings = &OrgSettings{Features: map[string]bool{}}
	}
	c.settingsMu.Lock()
	c.settings = settings
	c.settingsMu.Unlock()
	return settings, nil
}

// CachedSettings returns the last successfully fetched settings, or nil
// if none has been fetched yet. Returns a defensive copy of the Features
// map so the caller can iterate without holding the lock.
func (c *Client) CachedSettings() *OrgSettings {
	c.settingsMu.RLock()
	defer c.settingsMu.RUnlock()
	if c.settings == nil {
		return nil
	}
	cp := &OrgSettings{
		DisplayName: c.settings.DisplayName,
		Features:    make(map[string]bool, len(c.settings.Features)),
	}
	for k, v := range c.settings.Features {
		cp.Features[k] = v
	}
	return cp
}

// ScreenshotsEnabled is the canonical check the activity loop runs
// before each screenshot tick.
//
// Fail-closed: if no settings have been fetched yet, screenshots are
// suppressed. This matches the privacy contract — never capture without
// having confirmed the tenant opted in.
func (c *Client) ScreenshotsEnabled() bool {
	c.settingsMu.RLock()
	defer c.settingsMu.RUnlock()
	if c.settings == nil {
		return false
	}
	enabled, ok := c.settings.Features["screenshots"]
	if !ok {
		// Settings loaded but the key is absent — treat as disabled so a
		// new tenant who has not explicitly opted in stays opted out.
		return false
	}
	return enabled
}

// GetMyAttendance fetches GET /attendance/me.
func (c *Client) GetMyAttendance() (*Attendance, error) {
	req, err := c.request()
	if err != nil {
		return nil, err
	}

	resp, err := req.Get("/attendance/me")
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}

	if resp.StatusCode() == 404 || resp.StatusCode() == 204 {
		return nil, nil // No attendance record for today
	}

	if resp.StatusCode() != 200 {
		return nil, apiError("fetch attendance", resp)
	}

	// Backend returns 200 with body "null" when no attendance exists
	body := resp.Body()
	if len(body) == 0 || string(body) == "null" || string(body) == "{}" {
		return nil, nil
	}

	// Convert snake_case response to camelCase to match Go struct json tags
	converted, err := snakeToCamel(body)
	if err != nil {
		return nil, fmt.Errorf("attendance response: %w", err)
	}
	var attendance Attendance
	if err := json.Unmarshal(converted, &attendance); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &attendance, nil
}

// SignIn calls POST /attendance/sign-in.
func (c *Client) SignIn(data StartTimerData) (*Attendance, error) {
	req, err := c.request()
	if err != nil {
		return nil, err
	}

	// Build snake_case body, omitting empty fields (backend expects null, not "")
	body := make(map[string]interface{})
	if data.TaskID != "" {
		body["task_id"] = data.TaskID
	}
	if data.ProjectID != "" {
		body["project_id"] = data.ProjectID
	}
	if data.TaskTitle != "" {
		body["task_title"] = data.TaskTitle
	}
	if data.ProjectName != "" {
		body["project_name"] = data.ProjectName
	}
	if data.Description != "" {
		body["description"] = data.Description
	}

	resp, err := req.SetBody(body).Post("/attendance/sign-in")
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}

	if resp.StatusCode() != 201 {
		return nil, apiError("sign-in", resp)
	}

	// Convert snake_case response to camelCase and parse
	converted, err := snakeToCamel(resp.Body())
	if err != nil {
		return nil, fmt.Errorf("sign-in response: %w", err)
	}
	var attendance Attendance
	if err := json.Unmarshal(converted, &attendance); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &attendance, nil
}

// SignOut calls PUT /attendance/sign-out.
func (c *Client) SignOut() (*Attendance, error) {
	return c.SignOutContext(context.Background())
}

// SignOutContext is SignOut with a caller-supplied context. Used by
// auto-sign-out paths (tray Quit, OS shutdown, SIGTERM) that must
// bound the backend call to a few seconds so a slow network can't
// block process teardown. The shared resty Client's 30-second
// timeout is too generous for that case.
func (c *Client) SignOutContext(ctx context.Context) (*Attendance, error) {
	req, err := c.request()
	if err != nil {
		return nil, err
	}

	resp, err := req.SetContext(ctx).SetBody("{}").Put("/attendance/sign-out")
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, apiError("sign-out", resp)
	}

	converted, err := snakeToCamel(resp.Body())
	if err != nil {
		return nil, fmt.Errorf("sign-out response: %w", err)
	}
	var attendance Attendance
	if err := json.Unmarshal(converted, &attendance); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &attendance, nil
}

// GetMyTasks fetches GET /users/me/tasks.
func (c *Client) GetMyTasks() ([]Task, error) {
	req, err := c.request()
	if err != nil {
		// Auth gone (post-logout race): don't fall through to the
		// offline cache — it might leak another user's task list on
		// a shared machine. See V3-offline.
		return nil, err
	}

	resp, err := req.Get("/users/me/tasks")
	if err != nil {
		// Transport error (network down, DNS fail, TLS issue). Try
		// the on-disk cache so the user can still pick a task.
		if cached, ok := c.loadCachedTasks(); ok {
			log.Printf("GetMyTasks: network error (%v) — serving cached task list", err)
			return cached, nil
		}
		return nil, fmt.Errorf("network error: %w", err)
	}

	if resp.StatusCode() != 200 {
		// 4xx/5xx: the backend is reachable and said no. Don't
		// serve a stale cache — the live state is authoritative
		// for auth/permission errors.
		return nil, apiError("fetch tasks", resp)
	}

	converted, err := snakeToCamel(resp.Body())
	if err != nil {
		return nil, fmt.Errorf("tasks response: %w", err)
	}
	var tasks []Task
	if err := json.Unmarshal(converted, &tasks); err != nil {
		return nil, fmt.Errorf("failed to parse tasks: %w", err)
	}

	// Save the converted (camelCase) bytes so a next-launch cold
	// read unmarshals directly without running snakeToCamel again.
	if c.tasksCache != nil {
		if serr := c.tasksCache.Store(converted); serr != nil {
			log.Printf("GetMyTasks: cache store failed: %v", serr)
		}
	}

	return tasks, nil
}

// loadCachedTasks returns the last-known-good tasks from disk, or
// (nil, false) if there's nothing cached. Used as a fallback when
// the live fetch fails at transport level.
func (c *Client) loadCachedTasks() ([]Task, bool) {
	if c.tasksCache == nil {
		return nil, false
	}
	var tasks []Task
	ok, err := c.tasksCache.LoadInto(&tasks)
	if err != nil {
		log.Printf("loadCachedTasks: %v", err)
		return nil, false
	}
	if !ok || len(tasks) == 0 {
		return nil, false
	}
	return tasks, true
}

// ClearTasksCache wipes the on-disk task list. Called from Logout
// so a shared machine doesn't show user A's tasks to user B on a
// subsequent offline launch.
func (c *Client) ClearTasksCache() {
	if c.tasksCache == nil {
		return
	}
	// Overwriting with an empty list is simpler than deleting the
	// file — LoadInto treats [] as "no cached data" naturally.
	_ = c.tasksCache.Store([]byte("[]"))
}

// GetCurrentUser fetches GET /users/me.
func (c *Client) GetCurrentUser() (*User, error) {
	req, err := c.request()
	if err != nil {
		return nil, err
	}

	resp, err := req.Get("/users/me")
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, apiError("fetch user", resp)
	}

	converted, err := snakeToCamel(resp.Body())
	if err != nil {
		return nil, fmt.Errorf("user response: %w", err)
	}
	var user User
	if err := json.Unmarshal(converted, &user); err != nil {
		return nil, fmt.Errorf("failed to parse user: %w", err)
	}

	return &user, nil
}

// SendActivityHeartbeat calls POST /activity/heartbeat (Phase 2).
func (c *Client) SendActivityHeartbeat(data map[string]interface{}) error {
	req, err := c.request()
	if err != nil {
		return err
	}

	resp, err := req.SetBody(data).Post("/activity/heartbeat")
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}

	if resp.StatusCode() != 201 {
		return apiError("heartbeat", resp)
	}

	return nil
}

// UploadScreenshot uploads a screenshot to S3 via a presigned URL.
// Returns the CDN URL of the uploaded file.
//
// Trust-boundary notes:
//   - filename is URL-escaped before being joined into the presign query
//     string. A crafted filename containing "&contentType=text/html"
//     could otherwise override the contentType parameter we send next
//     in the same query. See C-API-2.
//   - presignResp.UploadURL is server-controlled. We validate it against
//     allowedUploadHosts before PUTting JPEG bytes to it, so a
//     compromised backend cannot redirect a frame of the user's screen
//     to an attacker host. See C-API-1.
//
// Retry behavior: if the S3 PUT returns 403 (most commonly because the
// presigned URL expired between fetch and upload on a slow uplink), we
// request a fresh presign and try the PUT once more. Idempotent — the
// key is derived from filename, so a retry overwrites rather than
// duplicating. See V3-H7.
func (c *Client) UploadScreenshot(jpegData []byte, filename string) (string, error) {
	cdnURL, err := c.uploadScreenshotOnce(jpegData, filename)
	if err == nil {
		return cdnURL, nil
	}
	// Only retry on the expired-presign signature — everything else is
	// either a real failure (network, auth) or a bug.
	if !isPresignExpired(err) {
		return "", err
	}
	log.Printf("screenshot upload: presign appears expired (%v) — requesting fresh URL and retrying once", err)
	return c.uploadScreenshotOnce(jpegData, filename)
}

// uploadScreenshotOnce performs one presign + PUT cycle.
func (c *Client) uploadScreenshotOnce(jpegData []byte, filename string) (string, error) {
	// Step 1: Get presigned URL
	req, err := c.request()
	if err != nil {
		return "", err
	}

	presignURL := fmt.Sprintf(
		"/uploads/presign?type=screenshot&filename=%s&contentType=image/jpeg",
		url.QueryEscape(filename),
	)
	resp, err := req.Get(presignURL)
	if err != nil {
		return "", fmt.Errorf("presign network error: %w", err)
	}
	if resp.StatusCode() != 200 {
		return "", apiError("upload presign", resp)
	}

	converted, err := snakeToCamel(resp.Body())
	if err != nil {
		return "", fmt.Errorf("presign response: %w", err)
	}
	var presignResp struct {
		UploadURL string `json:"uploadUrl"`
		FileURL   string `json:"fileUrl"`
	}
	if err := json.Unmarshal(converted, &presignResp); err != nil {
		return "", fmt.Errorf("failed to parse presign response: %w", err)
	}

	// Reject any upload URL that isn't https + in our allowlist. Without
	// this, a compromised backend can point screenshot PUTs at an
	// attacker-controlled host.
	uploadURL, err := security.ValidateHTTPSURL(presignResp.UploadURL, allowedUploadHosts)
	if err != nil {
		return "", fmt.Errorf("refusing screenshot upload: %w", err)
	}

	// Step 2: Upload directly to S3 (via the validated URL). Uses the
	// dedicated c.uploads client with a longer (3 min) timeout so a
	// multi-MB screenshot on a slow uplink isn't silently dropped by
	// the 30 s API-call budget. See M-API-2.
	s3Resp, err := c.uploads.R().
		SetHeader("Content-Type", "image/jpeg").
		SetBody(jpegData).
		Put(uploadURL.String())
	if err != nil {
		return "", fmt.Errorf("S3 upload network error: %w", err)
	}
	if s3Resp.StatusCode() != 200 {
		return "", fmt.Errorf("S3 upload failed %d", s3Resp.StatusCode())
	}

	return presignResp.FileURL, nil
}

// isPresignExpired returns true for errors that look like an expired
// presigned URL (S3 returns 403 "Request has expired" or similar).
// Used to decide whether UploadScreenshot's single retry path applies.
func isPresignExpired(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "S3 upload failed 403")
}

// snakeToCamel converts a backend response body's snake_case JSON keys
// into camelCase so it can be unmarshaled into our Go structs (whose
// json tags are camelCase).
//
// Handles both objects and arrays at the top level. Returns an error if
// the body is not JSON — previously we silently returned the raw bytes,
// which let HTML error pages from a WAF or reverse proxy slip through
// and surface downstream as a confusing "failed to parse" error.
// See H-API-3.
func snakeToCamel(data []byte) ([]byte, error) {
	// Try as object first
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err == nil {
		result, err := json.Marshal(convertKeys(raw))
		if err != nil {
			return nil, fmt.Errorf("re-marshal converted object: %w", err)
		}
		return result, nil
	}

	// Try as array
	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err == nil {
		result, err := json.Marshal(convertArray(arr))
		if err != nil {
			return nil, fmt.Errorf("re-marshal converted array: %w", err)
		}
		return result, nil
	}

	// Neither object nor array — almost always a proxy/WAF HTML error
	// page that somehow got through with a 2xx status. Cap the preview
	// so we don't drag a 10 KB page into the error string.
	preview := data
	if len(preview) > 80 {
		preview = preview[:80]
	}
	return nil, fmt.Errorf("expected JSON response, got %d bytes of non-JSON: %q", len(data), preview)
}

func convertArray(arr []interface{}) []interface{} {
	result := make([]interface{}, len(arr))
	for i, item := range arr {
		if obj, ok := item.(map[string]interface{}); ok {
			result[i] = convertKeys(obj)
		} else {
			result[i] = item
		}
	}
	return result
}

func convertKeys(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		newKey := toCamelCase(k)
		switch val := v.(type) {
		case map[string]interface{}:
			result[newKey] = convertKeys(val)
		case []interface{}:
			arr := make([]interface{}, len(val))
			for i, item := range val {
				if obj, ok := item.(map[string]interface{}); ok {
					arr[i] = convertKeys(obj)
				} else {
					arr[i] = item
				}
			}
			result[newKey] = arr
		default:
			result[newKey] = v
		}
	}
	return result
}

func toCamelCase(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}
