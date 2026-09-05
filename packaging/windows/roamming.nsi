; roamming - Windows installer (NSIS).
;
; Built by the release workflow on the windows runner:
;   makensis /DVERSION=1.2.3 /DEXE=<abs path to roamming.exe> ^
;            /DOUTFILE=<abs path to output exe> roamming.nsi
; (GoReleaser's built-in NSIS packaging is Pro-only, so we drive makensis
; directly.)
;
; Per-user install into $LOCALAPPDATA\Programs\roamming: no admin rights
; needed. Includes an opt-out "Start when I log in" component (HKCU Run)
; - the Windows equivalent of the Linux systemd user unit and the macOS
; LaunchAgent shipped by the other installers.

Unicode true
ManifestDPIAware true
RequestExecutionLevel user
SetCompressor /SOLID lzma
BrandingText "roamming - https://github.com/nathabonfim59/roamming"

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef EXE
  !define EXE "roamming.exe"
!endif
!ifndef OUTFILE
  !define OUTFILE "roamming-${VERSION}-setup.exe"
!endif

!define APP "roamming"
!define REGKEY "Software\roamming"
!define RUNKEY "Software\Microsoft\Windows\CurrentVersion\Run"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\roamming"

VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "roamming"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileDescription" "Roam activity tray app"
VIAddVersionKey "FileVersion" "${VERSION}.0"
VIAddVersionKey "LegalCopyright" "nathabonfim59"

Name "${APP} ${VERSION}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\${APP}"
InstallDirRegKey HKCU "${REGKEY}" "InstallDir"
ShowInstDetails show
ShowUnInstDetails show

!include "MUI2.nsh"

!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\roamming.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Start roamming now"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; A running instance locks the exe; close it before replacing/removing.
!macro KillApp
  ExecWait "taskkill /IM roamming.exe /F"
  Sleep 500
!macroend

Section "${APP}" SEC_MAIN
  SectionIn RO
  !insertmacro KillApp
  SetOutPath "$INSTDIR"
  File /oname=roamming.exe "${EXE}"
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  ; Start Menu shortcuts (current user)
  CreateDirectory "$SMPROGRAMS\${APP}"
  CreateShortcut "$SMPROGRAMS\${APP}\${APP}.lnk" "$INSTDIR\roamming.exe"
  CreateShortcut "$SMPROGRAMS\${APP}\Uninstall ${APP}.lnk" "$INSTDIR\Uninstall.exe"

  SetRegView 64
  WriteRegStr HKCU "${REGKEY}" "InstallDir" "$INSTDIR"
  ; Add/Remove Programs entry (per-user)
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "${APP}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "nathabonfim59"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\roamming.exe"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
SectionEnd

Section "Start ${APP} when I log in" SEC_AUTOSTART
  SetRegView 64
  WriteRegStr HKCU "${RUNKEY}" "${APP}" '"$INSTDIR\roamming.exe"'
SectionEnd

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SEC_AUTOSTART} "Launch roamming automatically when you log in (it lives in the system tray)."
!insertmacro MUI_FUNCTION_DESCRIPTION_END

Section "Uninstall"
  !insertmacro KillApp
  SetRegView 64
  DeleteRegValue HKCU "${RUNKEY}" "${APP}"
  DeleteRegKey /ifempty HKCU "${UNINSTKEY}"
  DeleteRegKey /ifempty HKCU "${REGKEY}"
  Delete "$SMPROGRAMS\${APP}\${APP}.lnk"
  Delete "$SMPROGRAMS\${APP}\Uninstall ${APP}.lnk"
  RMDir "$SMPROGRAMS\${APP}"
  Delete "$INSTDIR\roamming.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
SectionEnd
