; The Windows installer of the Kitties desktop app: the app and the gateway it starts, side by side,
; in the person's own folder (no administrator needed), with a Start menu entry and an uninstaller.
; Built in CI (makensis -DARCH=amd64 -DVERSION=v1.2.3 -DBIN=<folder with the two programs>
; -DOUT=<installer> [-DICON=<file.ico>] installer.nsi).

Unicode true
!include "MUI2.nsh"
!include "LogicLib.nsh"

!ifndef ARCH
  !define ARCH "amd64"
!endif
!ifndef VERSION
  !define VERSION "dev"
!endif
!ifndef BIN
  !define BIN "."
!endif
!ifndef OUT
  !define OUT "kitties-desktop-windows-${ARCH}-setup.exe"
!endif

!define APP "Kitties"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Kitties"
!define WEBVIEW2 "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

Name "${APP}"
OutFile "${OUT}"
InstallDir "$LOCALAPPDATA\Programs\${APP}"
InstallDirRegKey HKCU "${UNINSTKEY}" "InstallLocation"
RequestExecutionLevel user
SetCompressor /SOLID lzma
BrandingText "${APP} ${VERSION}"

!ifdef ICON
  !define MUI_ICON "${ICON}"
  !define MUI_UNICON "${ICON}"
!endif
!define MUI_FINISHPAGE_RUN "$INSTDIR\kitties-desktop.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open ${APP}"

!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "Spanish"

; Is the Edge WebView2 runtime (what draws the window) installed? Windows 11 has it; a Windows 10
; that was never updated may not.
Function HasWebView2
  Push $0
  SetRegView 64
  ReadRegStr $0 HKLM "${WEBVIEW2}" "pv"
  ${If} $0 == ""
    ReadRegStr $0 HKCU "${WEBVIEW2}" "pv"
  ${EndIf}
  SetRegView 32
  ${If} $0 == ""
    ReadRegStr $0 HKLM "${WEBVIEW2}" "pv"
  ${EndIf}
  ${If} $0 == "0.0.0.0"
    StrCpy $0 ""
  ${EndIf}
  Exch $0
FunctionEnd

Section "Kitties" SecApp
  ; A copy that is still running holds its files.
  nsExec::Exec 'taskkill /F /IM kitties-desktop.exe'
  Pop $0
  nsExec::Exec 'taskkill /F /IM kitties-gateway.exe'
  Pop $0
  Sleep 500

  SetOutPath "$INSTDIR"
  File "${BIN}\kitties-desktop.exe"
  File "${BIN}\kitties-gateway.exe"
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  CreateDirectory "$SMPROGRAMS\${APP}"
  CreateShortcut "$SMPROGRAMS\${APP}\${APP}.lnk" "$INSTDIR\kitties-desktop.exe"
  CreateShortcut "$DESKTOP\${APP}.lnk" "$INSTDIR\kitties-desktop.exe"

  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "${APP}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "madkoding"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\kitties-desktop.exe"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1

  Call HasWebView2
  Pop $0
  ${If} $0 == ""
    DetailPrint "Downloading the WebView2 runtime (the window of ${APP} is drawn with it)..."
    nsExec::ExecToLog 'powershell -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12; Invoke-WebRequest -UseBasicParsing https://go.microsoft.com/fwlink/p/?LinkId=2124703 -OutFile $$env:TEMP\MicrosoftEdgeWebview2Setup.exe"'
    Pop $0
    ${If} $0 == 0
      ExecWait '"$TEMP\MicrosoftEdgeWebview2Setup.exe" /silent /install' $0
      Delete "$TEMP\MicrosoftEdgeWebview2Setup.exe"
    ${EndIf}
    Call HasWebView2
    Pop $0
    ${If} $0 == ""
      MessageBox MB_ICONEXCLAMATION|MB_OK "${APP} needs the Microsoft Edge WebView2 runtime and it could not be installed.$\r$\nInstall it from https://developer.microsoft.com/microsoft-edge/webview2/ and open ${APP} again."
    ${EndIf}
  ${EndIf}
SectionEnd

Section "Uninstall"
  nsExec::Exec 'taskkill /F /IM kitties-desktop.exe'
  Pop $0
  nsExec::Exec 'taskkill /F /IM kitties-gateway.exe'
  Pop $0
  Sleep 500
  Delete "$INSTDIR\kitties-desktop.exe"
  Delete "$INSTDIR\kitties-gateway.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\${APP}\${APP}.lnk"
  RMDir "$SMPROGRAMS\${APP}"
  Delete "$DESKTOP\${APP}.lnk"
  DeleteRegKey HKCU "${UNINSTKEY}"
  ; The data (~/.kitties: chats, the token, the configuration) stays: it is the person's.
SectionEnd
