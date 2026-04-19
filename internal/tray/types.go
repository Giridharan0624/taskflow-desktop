package tray

import "net/url"

// ActionHandler defines callbacks for tray menu actions.
type ActionHandler struct {
	OnShowWindow func()
	OnStopTimer  func()
	OnQuit       func()
}

// isSafeBrowserURL returns true only for http(s) URLs without userinfo.
// Defense-in-depth before every openBrowser call site — config.Get()
// already validates WebDashboardURL at startup, but a URL typed into
// a dev config.json or future dynamic config source still flows here.
//
// Why strict http(s): macOS `open` and Windows ShellExecute will both
// dispatch arbitrary URI schemes (javascript:, file:, tel:, mailto:,
// custom-protocol://...) to whatever handler is registered. Only web
// URLs should open in a browser. See V2-M1.
func isSafeBrowserURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	if u.User != nil {
		return false
	}
	if u.Host == "" {
		return false
	}
	return true
}
