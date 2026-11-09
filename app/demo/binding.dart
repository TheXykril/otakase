import 'dart:js_interop';
import 'dart:ui' as ui;

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

@JS('parent.postMessage')
external void _toPage(JSString message, JSString origin);

/// Lets the demo act like a device inside the website's frame:
/// - a phone player asking for landscape tells the page, which turns the
///   phone frame sideways;
/// - Esc on a phone or TV is the Back button;
/// - the app's own Android/desktop channels answer nothing instead of
///   failing, as on a device without that feature.
class DemoBinding extends WidgetsFlutterBinding {
  DemoBinding._(this.device);
  final String device;

  static DemoBinding ensureInitialized(String device) {
    DemoBinding._(device);
    return WidgetsBinding.instance as DemoBinding;
  }

  @override
  void initInstances() {
    super.initInstances();
    if (device != 'computer') {
      HardwareKeyboard.instance.addHandler((event) {
        if (event is! KeyDownEvent ||
            event.logicalKey != LogicalKeyboardKey.escape) {
          return false;
        }
        handlePopRoute();
        return true;
      });
    }
  }

  @override
  BinaryMessenger createBinaryMessenger() =>
      _DemoMessenger(super.createBinaryMessenger());
}

class _DemoMessenger implements BinaryMessenger {
  _DemoMessenger(this.inner);
  final BinaryMessenger inner;
  static const _platform = JSONMethodCodec();

  @override
  Future<ByteData?>? send(String channel, ByteData? message) {
    if (channel == 'flutter/platform' && message != null) {
      final call = _platform.decodeMethodCall(message);
      if (call.method == 'SystemChrome.setPreferredOrientations') {
        final wanted = (call.arguments as List).cast<String>();
        final landscape =
            wanted.isNotEmpty && wanted.every((o) => o.contains('landscape'));
        try {
          _toPage(
            (landscape ? 'otakase-demo:landscape' : 'otakase-demo:portrait')
                .toJS,
            '*'.toJS,
          );
        } catch (_) {
          /* Opened on its own, not in the site's frame. */
        }
      }
    }
    if (channel.startsWith('otakase/') || channel == 'window_manager') {
      return _orNothing(inner.send(channel, message), message);
    }
    return inner.send(channel, message);
  }

  /// Answers calls nobody handles on the web: questions such as
  /// `isMaximized` get false, everything else nothing.
  static Future<ByteData?> _orNothing(
    Future<ByteData?>? reply,
    ByteData? message,
  ) async {
    final answer = await reply;
    if (answer != null || message == null) return answer;
    const codec = StandardMethodCodec();
    final method = codec.decodeMethodCall(message).method;
    return codec.encodeSuccessEnvelope(
      RegExp(r'^is[A-Z]').hasMatch(method) ? false : null,
    );
  }

  @override
  Future<void> handlePlatformMessage(
    String channel,
    ByteData? data,
    ui.PlatformMessageResponseCallback? callback,
    // ignore: deprecated_member_use
  ) => inner.handlePlatformMessage(channel, data, callback);

  @override
  void setMessageHandler(String channel, MessageHandler? handler) =>
      inner.setMessageHandler(channel, handler);
}
