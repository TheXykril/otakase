[Setup]
AppName=Otakase Installer
AppVersion=1.0.0
DefaultDirName={userappdata}\Otakase
PrivilegesRequired=lowest
AllowNoIcons=yes
OutputBaseFilename=otakase-windows-installer
UsePreviousAppDir=yes
Compression=lzma2
SolidCompression=yes

[Tasks]
; Define a task for creating a desktop shortcut
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Additional Options";

[Files]
; Copy the Otakase executable to the install directory
Source: "..\releases\otakase-{#SetupSetting("AppVersion")}\windows\otakase-windows-x86_64.exe"; DestDir: "{app}"; DestName: "otakase.exe"; Flags: ignoreversion
Source: "mpv\mpv.exe"; DestDir: "{app}\bin"; Flags: ignoreversion

[Icons]
; Create the application icon in the Start Menu
Name: "{group}\Otakase"; Filename: "{app}\otakase.exe"
; Create a desktop shortcut if the user checked the option
Name: "{userdesktop}\Otakase"; Filename: "{app}\otakase.exe"; Tasks: desktopicon
