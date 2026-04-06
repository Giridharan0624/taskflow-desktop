package monitor

import "strings"

// knownApps maps executable/process names to friendly display names.
// Covers Windows, Linux, and macOS application names.
var knownApps = map[string]string{
	// IDEs & Editors
	"Code":          "VS Code",
	"code":          "VS Code",
	"code-oss":      "VS Code",
	"devenv":        "Visual Studio",
	"idea64":        "IntelliJ IDEA",
	"idea":          "IntelliJ IDEA",
	"webstorm64":    "WebStorm",
	"webstorm":      "WebStorm",
	"goland64":      "GoLand",
	"goland":        "GoLand",
	"pycharm64":     "PyCharm",
	"pycharm":       "PyCharm",
	"sublime_text":  "Sublime Text",
	"atom":          "Atom",
	"notepad":       "Notepad",
	"gedit":         "gedit",
	"vim":           "Vim",
	"nvim":          "Neovim",
	"emacs":         "Emacs",
	"Xcode":         "Xcode",

	// Browsers
	"chrome":             "Chrome",
	"google-chrome":      "Chrome",
	"Google Chrome":      "Chrome",
	"chromium":           "Chromium",
	"chromium-browser":   "Chromium",
	"msedge":             "Edge",
	"Microsoft Edge":     "Edge",
	"firefox":            "Firefox",
	"Firefox":            "Firefox",
	"firefox-esr":        "Firefox",
	"Safari":             "Safari",
	"brave":              "Brave",
	"brave-browser":      "Brave",
	"opera":              "Opera",

	// Communication
	"slack":       "Slack",
	"Slack":       "Slack",
	"Discord":     "Discord",
	"discord":     "Discord",
	"Teams":       "Teams",
	"teams":       "Teams",
	"Telegram":    "Telegram",
	"telegram":    "Telegram",
	"zoom":        "Zoom",
	"Zoom":        "Zoom",

	// Terminals
	"WindowsTerminal":    "Terminal",
	"cmd":                "Command Prompt",
	"powershell":         "PowerShell",
	"pwsh":               "PowerShell",
	"gnome-terminal":     "Terminal",
	"gnome-terminal-server": "Terminal",
	"konsole":            "Terminal",
	"xfce4-terminal":     "Terminal",
	"alacritty":          "Alacritty",
	"kitty":              "Kitty",
	"wezterm-gui":        "WezTerm",
	"Terminal":           "Terminal",
	"iTerm2":             "iTerm",
	"iterm2":             "iTerm",

	// File Managers
	"explorer":   "File Explorer",
	"nautilus":   "Files",
	"dolphin":    "Dolphin",
	"thunar":     "Thunar",
	"nemo":       "Nemo",
	"Finder":     "Finder",

	// Office
	"WINWORD":            "Word",
	"EXCEL":              "Excel",
	"POWERPNT":           "PowerPoint",
	"OUTLOOK":            "Outlook",
	"libreoffice":        "LibreOffice",
	"soffice":            "LibreOffice",

	// Dev Tools
	"Postman":    "Postman",
	"postman":    "Postman",
	"figma":      "Figma",
	"Figma":      "Figma",
	"docker":     "Docker",
	"Docker":     "Docker",

	// Productivity
	"Notion":     "Notion",
	"notion":     "Notion",
	"Obsidian":   "Obsidian",
	"obsidian":   "Obsidian",

	// Desktop app
	"taskflow-desktop":     "TaskFlow Desktop",
	"taskflow-desktop-dev": "TaskFlow Desktop (Dev)",
}

// friendlyAppName converts an executable path or process name to a friendly display name.
func friendlyAppName(nameOrPath string) string {
	// Extract filename from path (handles both / and \)
	parts := strings.Split(strings.ReplaceAll(nameOrPath, "\\", "/"), "/")
	fileName := parts[len(parts)-1]

	// Remove common extensions
	for _, ext := range []string{".exe", ".EXE", ".app", ".AppImage"} {
		fileName = strings.TrimSuffix(fileName, ext)
	}

	if friendly, ok := knownApps[fileName]; ok {
		return friendly
	}

	return fileName
}
