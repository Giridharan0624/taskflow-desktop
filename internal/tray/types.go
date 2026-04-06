package tray

// ActionHandler defines callbacks for tray menu actions.
type ActionHandler struct {
	OnShowWindow func()
	OnStopTimer  func()
	OnQuit       func()
}
