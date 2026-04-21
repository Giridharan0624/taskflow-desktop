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
	// ServerTime is the backend's UTC ISO timestamp captured when it
	// built this response. Frontend uses it as a clock reference so
	// the Timer ticks against server time, not the local OS clock —
	// cross-device displays agree even when one device's clock has
	// drifted. Optional (omitempty) so old backends that don't emit
	// the field don't break deserialisation.
	ServerTime     string              `json:"serverTime,omitempty"`
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

// GetAttendance returns a deep copy of the current attendance data.
//
// The previous implementation leaked the stored pointer past RLock, so
// callers could mutate the inner struct (or the Sessions slice) without
// synchronization. Returning a deep copy is the only safe shape: the
// caller owns the result and can do whatever it likes with it, while
// SetAttendance writers remain protected by the lock. See H-CORE-4.
func (s *AppState) GetAttendance() *Attendance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.attendance == nil {
		return nil
	}

	copied := *s.attendance // struct copy — shallow

	// Deep-copy the mutable slice so subsequent append/mutation on the
	// returned value can't race SetAttendance.
	if s.attendance.Sessions != nil {
		copied.Sessions = append([]AttendanceSession(nil), s.attendance.Sessions...)
	}

	// CurrentSignInAt is *string — each caller gets its own pointer so
	// setting it to nil on the copy doesn't affect the stored original.
	if s.attendance.CurrentSignInAt != nil {
		v := *s.attendance.CurrentSignInAt
		copied.CurrentSignInAt = &v
	}

	// CurrentTask is *CurrentTask — deep-copy through one layer.
	if s.attendance.CurrentTask != nil {
		taskCopy := *s.attendance.CurrentTask
		copied.CurrentTask = &taskCopy
	}

	return &copied
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
