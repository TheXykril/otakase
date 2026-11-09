// mpv properties and commands on the media_kit player. The web build (the
// website's live demo) has no mpv, so there they do nothing.
export 'mpv_native.dart' if (dart.library.js_interop) 'mpv_web.dart';
