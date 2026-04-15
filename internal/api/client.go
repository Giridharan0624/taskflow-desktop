package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"taskflow-desktop/internal/auth"
	"taskflow-desktop/internal/config"
	"taskflow-desktop/internal/state"
)

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
	converted := snakeToCamel(body)
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
	converted := snakeToCamel(resp.Body())
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

	converted := snakeToCamel(resp.Body())
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

	converted := snakeToCamel(resp.Body())
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

	converted := snakeToCamel(resp.Body())
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

// UploadScreenshot uploads a screenshot to S3 via presigned URL.
// Returns the CDN URL of the uploaded file.
func (c *Client) UploadScreenshot(jpegData []byte, filename string) (string, error) {
	// Step 1: Get presigned URL
	req, err := c.request()
	if err != nil {
		return "", err
	}

	presignURL := fmt.Sprintf("/uploads/presign?type=screenshot&filename=%s&contentType=image/jpeg", filename)
	resp, err := req.Get(presignURL)
	if err != nil {
		return "", fmt.Errorf("presign network error: %w", err)
	}
	if resp.StatusCode() != 200 {
		return "", apiError("upload presign", resp)
	}

	converted := snakeToCamel(resp.Body())
	var presignResp struct {
		UploadURL string `json:"uploadUrl"`
		FileURL   string `json:"fileUrl"`
	}
	if err := json.Unmarshal(converted, &presignResp); err != nil {
		return "", fmt.Errorf("failed to parse presign response: %w", err)
	}

	// Step 2: Upload directly to S3
	s3Resp, err := c.http.R().
		SetHeader("Content-Type", "image/jpeg").
		SetBody(jpegData).
		Put(presignResp.UploadURL)
	if err != nil {
		return "", fmt.Errorf("S3 upload network error: %w", err)
	}
	if s3Resp.StatusCode() != 200 {
		return "", fmt.Errorf("S3 upload failed %d", s3Resp.StatusCode())
	}

	return presignResp.FileURL, nil
}

// snakeToCamel is a helper to convert snake_case JSON keys to camelCase.
// The backend returns snake_case; the frontend/Go structs use camelCase tags.
// Handles both objects and arrays at the top level.
func snakeToCamel(data []byte) []byte {
	// Try as object first
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err == nil {
		converted := convertKeys(raw)
		result, _ := json.Marshal(converted)
		return result
	}

	// Try as array
	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err == nil {
		converted := convertArray(arr)
		result, _ := json.Marshal(converted)
		return result
	}

	return data
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
