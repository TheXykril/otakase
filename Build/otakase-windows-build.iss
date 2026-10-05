[Setup]
AppName=Otakase Installer
AppVersion=1.0.0
DefaultDirName={userappdata}\Otakase
PrivilegesRequired=lowest
AllowNoIcons=yes
; The app icon on the installer itself and in Apps & features. The shortcuts
; take it from otakase.exe, which carries it.
SetupIconFile=app-icon\otakase.ico
UninstallDisplayIcon={app}\otakase.exe
OutputBaseFilename=otakase-windows-installer
UsePreviousAppDir=yes
Compression=lzma2
SolidCompression=yes
; The bundled otakase.exe and mpv.exe are 64-bit only.
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; Tell open programs the PATH changed, so a new terminal sees otakase at once.
ChangesEnvironment=yes

[Tasks]
; Define a task for creating a desktop shortcut
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Additional Options";
; Lets `otakase` and `otk` run from any terminal. Checked by default, so a
; silent install (winget) gets it too.
Name: "addtopath"; Description: "Add otakase to &PATH (run it from any terminal)"; GroupDescription: "Additional Options";

[Files]
; Copy the Otakase executable to the install directory
Source: "..\releases\otakase-{#SetupSetting("AppVersion")}\windows\otakase-windows-x86_64.exe"; DestDir: "{app}"; DestName: "otakase.exe"; Flags: ignoreversion
; otk is the short form; Windows has no symlink worth using here, so this
; one-line shim forwards to the real executable beside it.
Source: "otk.cmd"; DestDir: "{app}"; Flags: ignoreversion
Source: "mpv\mpv.exe"; DestDir: "{app}\bin"; Flags: ignoreversion

[Icons]
; Create the application icon in the Start Menu
Name: "{group}\Otakase"; Filename: "{app}\otakase.exe"
; Create a desktop shortcut if the user checked the option
Name: "{userdesktop}\Otakase"; Filename: "{app}\otakase.exe"; Tasks: desktopicon

[Code]
const
  EnvKey = 'Environment';

// True when Dir is one of the ;-separated entries of Path, ignoring case.
function PathHasDir(Path, Dir: string): Boolean;
begin
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') > 0;
end;

// Appends the install folder to the user's PATH, not the machine's, so no
// admin rights are needed. Run again on upgrade, it finds the entry and stops.
procedure AddAppToPath;
var
  Path, Dir: string;
begin
  Dir := ExpandConstant('{app}');
  if not RegQueryStringValue(HKCU, EnvKey, 'Path', Path) then
    Path := '';
  if PathHasDir(Path, Dir) then
    Exit;
  if (Path <> '') and (Path[Length(Path)] <> ';') then
    Path := Path + ';';
  RegWriteExpandStringValue(HKCU, EnvKey, 'Path', Path + Dir);
end;

// Takes the install folder back out of the user's PATH, leaving every other
// entry as it was.
procedure RemoveAppFromPath;
var
  Path, Dir: string;
  P: Integer;
begin
  Dir := ExpandConstant('{app}');
  if not RegQueryStringValue(HKCU, EnvKey, 'Path', Path) then
    Exit;
  Path := ';' + Path + ';';
  P := Pos(';' + Uppercase(Dir) + ';', Uppercase(Path));
  if P = 0 then
    Exit;
  Delete(Path, P, Length(Dir) + 1);
  Path := Copy(Path, 2, Length(Path) - 2);
  RegWriteExpandStringValue(HKCU, EnvKey, 'Path', Path);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and WizardIsTaskSelected('addtopath') then
    AddAppToPath;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    RemoveAppFromPath;
end;
