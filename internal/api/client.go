package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"taskflow-desktop/internal/auth"
	"taskflow-desktop/internal/config"
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

// Client handles all API communication with the backend.
type Client struct {
	http        *resty.Client
	authService *auth.Service
	appState    *state.AppState
}

// NewClient creates a new API client with HTTPS enforced and TLS 1.3 minimum.
func NewClient(authService *auth.Service, appState *state.AppState) *Client {
	cfg := config.Get()

	// Enforce HTTPS — reject HTTP URLs
	if !strings.HasPrefix(cfg.APIURL, "https://") {
		panic("API URL must use HTTPS — refusing to start with insecure connection")
	}

	// TLS 1.3 minimum, no insecure ciphers
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	}

	client := resty.NewWithClient(&http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}).
		SetBaseURL(cfg.APIURL).
		SetHeader("Content-Type", "application/json")

	return &Client{
		http:        client,
		authService: authService,
		appState:    appState,
	}
}

// request creates an authenticated request with auto-refreshed JWT.
func (c *Client) request() (*resty.Request, error) {
	token, err := c.authService.GetIDToken()
	if err != nil {
		return nil, fmt.Errorf("not authenticated: %w", err)
	}

	return c.http.R().
		SetHeader("Authorization", "Bearer "+token), nil
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
	req, err := c.request()
	if err != nil {
		return nil, err
	}

	resp, err := req.SetBody("{}").Put("/attendance/sign-out")
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
		return nil, err
	}

	resp, err := req.Get("/users/me/tasks")
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}

	if resp.StatusCode() != 200 {
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

	return tasks, nil
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
func (c *Client) UploadScreenshot(jpegData []byte, filename string) (string, error) {
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

	// Step 2: Upload directly to S3 (via the validated URL).
	s3Resp, err := c.http.R().
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
