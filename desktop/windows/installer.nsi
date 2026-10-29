; The Windows installer of the Kitties desktop app: the app and the gateway it starts, side by side,
; in the person's own folder (no administrator needed), with a Start menu entry and an uninstaller.
; Built in CI (makensis -DARCH=amd64 -DVERSION=v1.2.3 -DBIN=<folder with the two programs>
; -DOUT=<installer> [-DICON=<file.ico>] installer.nsi).
; The installer asks where the gateway may be reached from (local, lan or wan); silently, pass
; /EXPOSE=local|lan|wan. The choice is kept in %USERPROFILE%\.kitties\exposure, which the gateway reads.

Unicode true
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "nsDialogs.nsh"
!include "FileFunc.nsh"

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

Var Expose
Var RadioLocal
Var RadioLan
Var RadioWan


Function .onInit
  StrCpy $Expose "local"
  ; a previous choice is kept when the installer runs again
  FileOpen $0 "$PROFILE\.kitties\exposure" r
  ${IfNot} ${Errors}
    FileRead $0 $1
    FileClose $0
    ${If} $1 == "lan$\n"
    ${OrIf} $1 == "lan$\r$\n"
    ${OrIf} $1 == "lan"
      StrCpy $Expose "lan"
    ${ElseIf} $1 == "wan$\n"
    ${OrIf} $1 == "wan$\r$\n"
    ${OrIf} $1 == "wan"
      StrCpy $Expose "wan"
    ${EndIf}
  ${EndIf}
  ClearErrors
  ${GetParameters} $0
  ${GetOptions} $0 "/EXPOSE=" $1
  ${If} $1 == "local"
  ${OrIf} $1 == "lan"
  ${OrIf} $1 == "wan"
    StrCpy $Expose $1
  ${EndIf}
FunctionEnd

Function ExposePage
  !insertmacro MUI_HEADER_TEXT "$(ExposeTitle)" "$(ExposeSub)"
  nsDialogs::Create 1018
  Pop $0
  ${NSD_CreateRadioButton} 0 10u 100% 12u "$(ExposeLocal)"
  Pop $RadioLocal
  ${NSD_CreateRadioButton} 0 30u 100% 12u "$(ExposeLan)"
  Pop $RadioLan
  ${NSD_CreateRadioButton} 0 50u 100% 12u "$(ExposeWan)"
  Pop $RadioWan
  ${If} $Expose == "lan"
    ${NSD_Check} $RadioLan
  ${ElseIf} $Expose == "wan"
    ${NSD_Check} $RadioWan
  ${Else}
    ${NSD_Check} $RadioLocal
  ${EndIf}
  nsDialogs::Show
FunctionEnd

Function ExposePageLeave
  ${NSD_GetState} $RadioWan $0
  ${NSD_GetState} $RadioLan $1
  ${If} $0 == ${BST_CHECKED}
    MessageBox MB_ICONEXCLAMATION|MB_YESNO|MB_DEFBUTTON2 "$(ExposeWarn)" IDYES +2
    Abort
    StrCpy $Expose "wan"
  ${ElseIf} $1 == ${BST_CHECKED}
    StrCpy $Expose "lan"
  ${Else}
    StrCpy $Expose "local"
  ${EndIf}
FunctionEnd

!insertmacro MUI_PAGE_DIRECTORY
Page custom ExposePage ExposePageLeave
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "Spanish"

LangString ExposeTitle ${LANG_ENGLISH} "Where can ${APP} be reached from?"
LangString ExposeTitle ${LANG_SPANISH} "¿Desde dónde se podrá llegar a ${APP}?"
LangString ExposeSub ${LANG_ENGLISH} "You can change it later in Settings."
LangString ExposeSub ${LANG_SPANISH} "Puedes cambiarlo después en Ajustes."
LangString ExposeLocal ${LANG_ENGLISH} "1) Local (127.0.0.1): only this computer. Safest, recommended."
LangString ExposeLocal ${LANG_SPANISH} "1) Local (127.0.0.1): solo este equipo. Lo más seguro, recomendado."
LangString ExposeLan ${LANG_ENGLISH} "2) LAN (this computer's address in your network): other devices at home or the office."
LangString ExposeLan ${LANG_SPANISH} "2) LAN (la dirección de este equipo en tu red): otros equipos de tu casa u oficina."
LangString ExposeWan ${LANG_ENGLISH} "3) WAN (0.0.0.0): every network interface, the internet too if this computer is public."
LangString ExposeWan ${LANG_SPANISH} "3) WAN (0.0.0.0): todas las interfaces de red, también internet si este equipo es público."
LangString ExposeWarn ${LANG_ENGLISH} "WAN listens on EVERY network interface. The page and its token travel over plain http and the token is the only password.$\r$\nUse it only behind a firewall or a VPN.$\r$\n$\r$\nListen on every interface anyway?"
LangString ExposeWarn ${LANG_SPANISH} "WAN escucha en TODAS las interfaces de red. La página y su token viajan por http sin cifrar y el token es la única contraseña.$\r$\nÚsalo solo tras un firewall o una VPN.$\r$\n$\r$\n¿Escuchar en todas las interfaces de todos modos?"

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

  ; Where the gateway listens (local, lan or wan); the gateway reads it from its data folder.
  CreateDirectory "$PROFILE\.kitties"
  FileOpen $0 "$PROFILE\.kitties\exposure" w
  FileWrite $0 "$Expose$\r$\n"
  FileClose $0

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
