package monitor

import (
	"fmt"
	"os/exec"
)

// ShowWindowsToast displays a Windows 10/11 toast notification using PowerShell.
// This creates the same style notification as WhatsApp, Teams, etc.
func ShowWindowsToast(title, message string) {
	// PowerShell script to show a toast notification via Windows.UI.Notifications
	script := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom, ContentType = WindowsRuntime] | Out-Null

$template = @"
<toast duration="short">
    <visual>
        <binding template="ToastGeneric">
            <text>%s</text>
            <text>%s</text>
        </binding>
    </visual>
    <audio silent="true"/>
</toast>
"@

$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml($template)
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("TaskFlow Desktop").Show($toast)
`, title, message)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	_ = cmd.Start() // Fire and forget — don't block
}
