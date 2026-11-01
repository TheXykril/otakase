; Installer for the Otakase desktop app (Windows). Built by
; scripts/package-desktop.sh: ISCC /DAppVersion=<version> otakase-app.iss
;
; Per user, so neither installing nor updating asks for admin rights. The
; app updates itself by running the next release's installer with
; /VERYSILENT /WAITPID=<its process id>: setup waits for the app to quit,
; replaces the files and opens the app again.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
; Never change AppId: setup finds the installed copy by it.
AppId={{C925A711-4CAB-4F14-9A95-021505776270}
AppName=Otakase App
AppVersion={#AppVersion}
AppVerName=Otakase App {#AppVersion}
AppPublisher=Xykril
DefaultDirName={localappdata}\Programs\Otakase App
DefaultGroupName=Otakase
DisableProgramGroupPage=yes
UsePreviousAppDir=yes
PrivilegesRequired=lowest
SetupIconFile=..\..\..\Build\app-icon\otakase.ico
UninstallDisplayIcon={app}\otakase.exe
UninstallDisplayName=Otakase App
OutputDir=..\..\build\desktop-release
OutputBaseFilename=otakase-app-windows-setup
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; Files still in use after the wait are closed rather than failing setup.
CloseApplications=force
RestartApplications=no

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Additional Options"; Flags: unchecked

[Files]
; Everything flutter build windows made, plus otakase-core.exe beside it.
Source: "..\..\build\windows\x64\runner\Release\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[InstallDelete]
; Files a newer version no longer has (old Flutter assets) go first.
Type: filesandordirs; Name: "{app}\data"

[Icons]
Name: "{group}\Otakase"; Filename: "{app}\otakase.exe"
Name: "{userdesktop}\Otakase"; Filename: "{app}\otakase.exe"; Tasks: desktopicon

[Run]
; Shown as a checkbox after a normal install. A silent update (the app's own
; updater) runs it too, which opens the new version.
Filename: "{app}\otakase.exe"; Description: "Open Otakase"; Flags: nowait postinstall

[Code]
const
  SYNCHRONIZE = $00100000;

function OpenProcess(DesiredAccess: Cardinal; InheritHandle: Boolean; ProcessId: Cardinal): THandle;
  external 'OpenProcess@kernel32.dll stdcall';
function WaitForSingleObject(Handle: THandle; Milliseconds: Cardinal): Cardinal;
  external 'WaitForSingleObject@kernel32.dll stdcall';
function CloseHandle(Handle: THandle): Boolean;
  external 'CloseHandle@kernel32.dll stdcall';

// The in-app updater passes /WAITPID=<pid> and quits right after starting
// setup. Wait (up to a minute) until it is gone, so no file is in use.
function InitializeSetup(): Boolean;
var
  Pid: Cardinal;
  Handle: THandle;
begin
  Pid := StrToIntDef(ExpandConstant('{param:WAITPID|0}'), 0);
  if Pid <> 0 then
  begin
    Handle := OpenProcess(SYNCHRONIZE, False, Pid);
    if Handle <> 0 then
    begin
      WaitForSingleObject(Handle, 60000);
      CloseHandle(Handle);
    end;
  end;
  Result := True;
end;
