Unicode true

!include "MUI2.nsh"
!include "x64.nsh"
!include "WinVer.nsh"
!include "FileFunc.nsh"

;; ─── General ───
!define PRODUCT_NAME "TaskFlow Desktop"
!define PRODUCT_VERSION "1.0.0"
!define PRODUCT_PUBLISHER "NEUROSTACK"
!define PRODUCT_WEB_SITE "https://taskflow-ns.vercel.app"
!define PRODUCT_EXE "taskflow-desktop.exe"
!define PRODUCT_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${PRODUCT_NAME}"
!define PRODUCT_AUTORUN_KEY "Software\Microsoft\Windows\CurrentVersion\Run"

Name "${PRODUCT_NAME} ${PRODUCT_VERSION}"
OutFile "TaskFlowDesktop-Setup-${PRODUCT_VERSION}.exe"
InstallDir "$PROGRAMFILES64\${PRODUCT_NAME}"
InstallDirRegKey HKLM "${PRODUCT_UNINST_KEY}" "InstallLocation"
RequestExecutionLevel admin
SetCompressor /SOLID lzma

;; ─── MUI Settings ───
!define MUI_ABORTWARNING
!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"

;; Welcome page
!define MUI_WELCOMEPAGE_TITLE "Welcome to ${PRODUCT_NAME} Setup"
!define MUI_WELCOMEPAGE_TEXT "This wizard will install ${PRODUCT_NAME} ${PRODUCT_VERSION} on your computer.$\r$\n$\r$\n${PRODUCT_NAME} is a lightweight time tracker and activity monitor for your organization's TaskFlow system.$\r$\n$\r$\nClick Next to continue."

;; ─── Pages ───

; 1. Welcome
!insertmacro MUI_PAGE_WELCOME

; 2. Privacy/Consent Notice (user must agree)
!insertmacro MUI_PAGE_LICENSE "privacy.txt"

; 3. Install Directory
!insertmacro MUI_PAGE_DIRECTORY

; 4. Options (shortcuts + autostart)
Page custom OptionsPage OptionsPageLeave

; 5. Install
!insertmacro MUI_PAGE_INSTFILES

; 6. Finish
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "Launch ${PRODUCT_NAME}"
!insertmacro MUI_PAGE_FINISH

; Uninstaller pages
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

;; ─── Language ───
!insertmacro MUI_LANGUAGE "English"

;; ─── Variables ───
Var DesktopShortcut
Var StartMenuShortcut
Var AutoStart

;; ─── Options Page ───
Function OptionsPage
    nsDialogs::Create 1018
    Pop $0

    ${NSD_CreateLabel} 0 0 100% 20u "Choose additional options:"
    Pop $0

    ${NSD_CreateCheckbox} 10u 30u 100% 15u "Create Desktop shortcut"
    Pop $DesktopShortcut
    ${NSD_Check} $DesktopShortcut

    ${NSD_CreateCheckbox} 10u 50u 100% 15u "Create Start Menu shortcut"
    Pop $StartMenuShortcut
    ${NSD_Check} $StartMenuShortcut

    ${NSD_CreateCheckbox} 10u 70u 100% 15u "Launch on Windows startup"
    Pop $AutoStart

    nsDialogs::Show
FunctionEnd

Function OptionsPageLeave
    ${NSD_GetState} $DesktopShortcut $DesktopShortcut
    ${NSD_GetState} $StartMenuShortcut $StartMenuShortcut
    ${NSD_GetState} $AutoStart $AutoStart
FunctionEnd

;; ─── Install Section ───
Section "Install"
    SetOutPath "$INSTDIR"

    ; Main application
    File "..\..\bin\${PRODUCT_EXE}"


    ; Create uninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"

    ; ── Register in Add/Remove Programs ──
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "DisplayName" "${PRODUCT_NAME}"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "UninstallString" "$INSTDIR\uninstall.exe"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXE}"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "DisplayVersion" "${PRODUCT_VERSION}"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "URLInfoAbout" "${PRODUCT_WEB_SITE}"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegDWORD HKLM "${PRODUCT_UNINST_KEY}" "NoModify" 1
    WriteRegDWORD HKLM "${PRODUCT_UNINST_KEY}" "NoRepair" 1

    ; Calculate installed size
    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    WriteRegDWORD HKLM "${PRODUCT_UNINST_KEY}" "EstimatedSize" "$0"

    ; ── Desktop Shortcut ──
    ${If} $DesktopShortcut == ${BST_CHECKED}
        CreateShortCut "$DESKTOP\${PRODUCT_NAME}.lnk" "$INSTDIR\${PRODUCT_EXE}" "" "$INSTDIR\${PRODUCT_EXE}" 0
    ${EndIf}

    ; ── Start Menu Shortcut ──
    ${If} $StartMenuShortcut == ${BST_CHECKED}
        CreateDirectory "$SMPROGRAMS\${PRODUCT_NAME}"
        CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\${PRODUCT_NAME}.lnk" "$INSTDIR\${PRODUCT_EXE}" "" "$INSTDIR\${PRODUCT_EXE}" 0
        CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\Uninstall.lnk" "$INSTDIR\uninstall.exe"
    ${EndIf}

    ; ── Auto-Start on Boot ──
    ${If} $AutoStart == ${BST_CHECKED}
        WriteRegStr HKCU "${PRODUCT_AUTORUN_KEY}" "${PRODUCT_NAME}" "$INSTDIR\${PRODUCT_EXE}"
    ${EndIf}

SectionEnd

;; ─── WebView2 Check ───
Section "WebView2 Runtime" SEC_WEBVIEW2
    ; Check if WebView2 is already installed
    ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    ${If} $0 == ""
        ReadRegStr $0 HKLM "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    ${EndIf}

    ${If} $0 == ""
        ; WebView2 not found — download and install
        DetailPrint "Installing WebView2 Runtime..."
        NSISdl::download "https://go.microsoft.com/fwlink/p/?LinkId=2124703" "$TEMP\MicrosoftEdgeWebview2Setup.exe"
        ExecWait "$TEMP\MicrosoftEdgeWebview2Setup.exe /silent /install"
        Delete "$TEMP\MicrosoftEdgeWebview2Setup.exe"
    ${Else}
        DetailPrint "WebView2 Runtime already installed (v$0)"
    ${EndIf}
SectionEnd

;; ─── Uninstall Section ───
Section "Uninstall"
    ; Kill running instance
    nsExec::ExecToLog 'taskkill /F /IM ${PRODUCT_EXE}'

    ; Remove files
    Delete "$INSTDIR\${PRODUCT_EXE}"
    Delete "$INSTDIR\uninstall.exe"
    RMDir "$INSTDIR"

    ; Remove shortcuts
    Delete "$DESKTOP\${PRODUCT_NAME}.lnk"
    RMDir /r "$SMPROGRAMS\${PRODUCT_NAME}"

    ; Remove registry entries
    DeleteRegKey HKLM "${PRODUCT_UNINST_KEY}"
    DeleteRegValue HKCU "${PRODUCT_AUTORUN_KEY}" "${PRODUCT_NAME}"

SectionEnd
