import 'package:media_kit/media_kit.dart';

extension Mpv on Player {
  bool get isMpv => false;
  Future<void> mpvSet(String name, String value) async {}
  Future<String?> mpvGet(String name) async => null;
  Future<void> mpvCommand(List<String> args) async {}
}
