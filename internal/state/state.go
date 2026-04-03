package state

import (
	"sync"
)

// Attendance mirrors the backend attendance response.
type Attendance struct {
	UserID         string              `json:"userId"`
	Date           string              `json:"date"`
	Sessions       []AttendanceSession `json:"sessions"`
	TotalHours     float64             `json:"totalHours"`
	CurrentSignInAt *string            `json:"currentSignInAt"`
	CurrentTask    *CurrentTask        `json:"currentTask"`
	UserName       string              `json:"userName"`
	UserEmail      string              `json:"userEmail"`
	SystemRole     string              `json:"systemRole"`
	Status         string              `json:"status"` // SIGNED_IN or SIGNED_OUT
	SessionCount   int                 `json:"sessionCount"`
}

// AttendanceSession represents a single work session.
type AttendanceSession struct {
	SignInAt    string   `json:"signInAt"`
	SignOutAt   *string  `json:"signOutAt"`
	Hours       *float64 `json:"hours"`
	TaskID      *string  `json:"taskId"`
	ProjectID   *string  `json:"projectId"`
	TaskTitle   *string  `json:"taskTitle"`
	ProjectName *string  `json:"projectName"`
	Description *string  `json:"description"`
}

// CurrentTask represents the currently tracked task.
type CurrentTask struct {
	TaskID      string `json:"taskId"`
	ProjectID   string `json:"projectId"`
	TaskTitle   string `json:"taskTitle"`
	ProjectName string `json:"projectName"`
}

// AppState holds the shared application state, safe for concurrent access.
type AppState struct {
	mu            sync.RWMutex
	authenticated bool
	attendance    *Attendance
	idleSeconds   int
}

// New creates a new AppState.
func New() *AppState {
	return &AppState{}
}

// IsAuthenticated returns whether the user is logged in.
func (s *AppState) IsAuthenticated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authenticated
}

// SetAuthenticated updates the authentication state.
func (s *AppState) SetAuthenticated(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authenticated = v
}

// GetAttendance returns the current attendance data.
func (s *AppState) GetAttendance() *Attendance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.attendance
}

// SetAttendance updates the current attendance data.
func (s *AppState) SetAttendance(a *Attendance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attendance = a
}

// IsTimerActive returns true if the user has an active timer.
func (s *AppState) IsTimerActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.attendance != nil && s.attendance.Status == "SIGNED_IN"
}

// GetIdleSeconds returns the current idle duration.
func (s *AppState) GetIdleSeconds() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idleSeconds
}

// SetIdleSeconds updates the idle duration.
func (s *AppState) SetIdleSeconds(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idleSeconds = seconds
}
