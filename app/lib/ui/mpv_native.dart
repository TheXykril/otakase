import 'package:media_kit/media_kit.dart';

extension Mpv on Player {
  /// False where the player is not mpv.
  bool get isMpv => platform is NativePlayer;

  Future<void> mpvSet(String name, String value) async {
    final native = platform;
    if (native is NativePlayer) await native.setProperty(name, value);
  }

  Future<String?> mpvGet(String name) async {
    final native = platform;
    return native is NativePlayer ? native.getProperty(name) : null;
  }

  Future<void> mpvCommand(List<String> args) async {
    final native = platform;
    if (native is NativePlayer) await native.command(args);
  }
}
