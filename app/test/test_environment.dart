import 'package:otakase/host.dart';

/// Variables a desktop session sets that change how the app looks or acts:
/// the compositor, the desktop name and the display it draws on.
const sessionVariables = {
  'HYPRLAND_INSTANCE_SIGNATURE',
  'SWAYSOCK',
  'I3SOCK',
  'NIRI_SOCKET',
  'XDG_CURRENT_DESKTOP',
  'XDG_SESSION_DESKTOP',
  'DESKTOP_SESSION',
  'XDG_SESSION_TYPE',
  'XDG_SESSION_ID',
  'WAYLAND_DISPLAY',
  'DISPLAY',
  'SESSIONNAME',
  'APPIMAGE',
};

/// The environment with the desktop session and Otakase's own overrides
/// taken out, so a test sees the same thing on every machine.
Map<String, String> testEnvironment(Map<String, String> env) => {
  for (final MapEntry(:key, :value) in env.entries)
    if (!sessionVariables.contains(key) && !key.startsWith('OTAKASE_'))
      key: value,
};

/// Called for every test file by flutter_test_config.dart.
void pinTestHost(Map<String, String> env) {
  Host.debugEnvironment = testEnvironment(env);
  Host.debugTimeZoneName = 'UTC';
}
