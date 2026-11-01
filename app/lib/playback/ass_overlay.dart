import 'dart:async';
import 'dart:convert';
import 'dart:ffi' hide Size;
import 'dart:isolate';
import 'dart:ui' as ui;

import 'package:ffi/ffi.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

/// True when [text] is an ASS/SSA subtitle file.
bool looksLikeAss(String text) {
  final head = text.trimLeft();
  return head.startsWith('[Script Info]') ||
      (text.contains('[Events]') && text.contains('Dialogue:'));
}

/// One rendered subtitle image, in physical pixels of the video rectangle.
class AssFrame {
  AssFrame(this.image, this.rect, this.size);
  final ui.Image image;
  final Rect rect;
  final Size size;
}

/// Full ASS rendering (styles, positions, karaoke, signs) for the Samsung TV
/// player, where AVPlay can't draw them. libass runs in libotakase_ass.so
/// (app/native/otakase_ass.c) on a worker isolate; each changed frame comes
/// back as an image drawn over the video by [AssLayer].
class AssOverlay {
  AssOverlay._(this._commands, this._isolate);

  final SendPort _commands;
  final Isolate _isolate;
  final frame = ValueNotifier<AssFrame?>(null);
  int _width = 0, _height = 0;
  bool _busy = false, _closed = false;
  Duration? _pending;

  static Future<AssOverlay?> create(String library, String text) async {
    try {
      final font = await rootBundle.load('assets/fonts/NotoSansJP-Regular.otf');
      final replies = ReceivePort();
      final isolate = await Isolate.spawn(_worker, replies.sendPort);
      final events = StreamIterator(replies);
      if (!await events.moveNext()) return null;
      final commands = events.current as SendPort;
      commands.send([
        'load',
        library,
        TransferableTypedData.fromList([font.buffer.asUint8List()]),
        TransferableTypedData.fromList([utf8.encode(text)]),
      ]);
      if (!await events.moveNext() || events.current != 'ok') {
        replies.close();
        isolate.kill();
        return null;
      }
      final overlay = AssOverlay._(commands, isolate);
      () async {
        while (await events.moveNext()) {
          overlay._reply(events.current);
        }
      }();
      return overlay;
    } catch (_) {
      return null;
    }
  }

  /// Size of the video rectangle in physical pixels.
  void resize(int width, int height) {
    if (width == _width && height == _height) return;
    _width = width;
    _height = height;
    frame.value = null;
  }

  /// Draws the subtitles for [position]. Skips while a frame is rendering;
  /// the newest position is drawn next.
  void show(Duration position) {
    if (_closed || _width <= 0 || _height <= 0) return;
    if (_busy) {
      _pending = position;
      return;
    }
    _busy = true;
    _commands.send(['render', position.inMilliseconds, _width, _height]);
  }

  Future<void> _reply(Object? message) async {
    if (message is! List) return;
    final status = message[0] as int;
    final size = Size(
      (message[1] as int).toDouble(),
      (message[2] as int).toDouble(),
    );
    if (status == 1 && !_closed) {
      final x = message[3] as int, y = message[4] as int;
      final w = message[5] as int, h = message[6] as int;
      final pixels = (message[7] as TransferableTypedData)
          .materialize()
          .asUint8List();
      final completer = Completer<ui.Image>();
      ui.decodeImageFromPixels(
        pixels,
        w,
        h,
        ui.PixelFormat.rgba8888,
        completer.complete,
      );
      final image = await completer.future;
      if (_closed || size.width != _width || size.height != _height) {
        image.dispose();
      } else {
        final old = frame.value;
        frame.value = AssFrame(
          image,
          Rect.fromLTWH(x.toDouble(), y.toDouble(), w.toDouble(), h.toDouble()),
          size,
        );
        old?.image.dispose();
      }
    } else if (status == 2) {
      final old = frame.value;
      frame.value = null;
      old?.image.dispose();
    }
    _busy = false;
    final next = _pending;
    _pending = null;
    if (next != null) show(next);
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    _commands.send(const ['close']);
    Timer(const Duration(seconds: 2), () => _isolate.kill());
    frame.value?.image.dispose();
    frame.value = null;
  }
}

/// Draws an [AssOverlay] over the video it is laid out on.
class AssLayer extends StatelessWidget {
  const AssLayer(this.overlay, {super.key});
  final AssOverlay overlay;

  @override
  Widget build(BuildContext context) {
    final ratio = MediaQuery.devicePixelRatioOf(context);
    return LayoutBuilder(
      builder: (context, constraints) {
        overlay.resize(
          (constraints.maxWidth * ratio).round(),
          (constraints.maxHeight * ratio).round(),
        );
        return ValueListenableBuilder<AssFrame?>(
          valueListenable: overlay.frame,
          builder: (context, frame, _) {
            if (frame == null) return const SizedBox.expand();
            return Stack(
              children: [
                Positioned(
                  left: frame.rect.left / ratio,
                  top: frame.rect.top / ratio,
                  width: frame.rect.width / ratio,
                  height: frame.rect.height / ratio,
                  child: RawImage(image: frame.image, fit: BoxFit.fill),
                ),
              ],
            );
          },
        );
      },
    );
  }
}

typedef _NewC = Pointer<Void> Function();
typedef _AddFontC = Void Function(
  Pointer<Void>,
  Pointer<Utf8>,
  Pointer<Uint8>,
  Int32,
);
typedef _AddFont = void Function(
  Pointer<Void>,
  Pointer<Utf8>,
  Pointer<Uint8>,
  int,
);
typedef _DefaultFontC = Void Function(Pointer<Void>, Pointer<Utf8>);
typedef _DefaultFont = void Function(Pointer<Void>, Pointer<Utf8>);
typedef _LoadC = Int32 Function(Pointer<Void>, Pointer<Uint8>, Int32);
typedef _Load = int Function(Pointer<Void>, Pointer<Uint8>, int);
typedef _SizeC = Void Function(Pointer<Void>, Int32, Int32);
typedef _Size = void Function(Pointer<Void>, int, int);
typedef _RenderC = Int32 Function(
  Pointer<Void>,
  Int64,
  Pointer<Int32>,
  Pointer<Int32>,
  Pointer<Int32>,
  Pointer<Int32>,
  Pointer<Pointer<Uint8>>,
);
typedef _Render = int Function(
  Pointer<Void>,
  int,
  Pointer<Int32>,
  Pointer<Int32>,
  Pointer<Int32>,
  Pointer<Int32>,
  Pointer<Pointer<Uint8>>,
);
typedef _FreeC = Void Function(Pointer<Void>);
typedef _Free = void Function(Pointer<Void>);

Pointer<Uint8> _copy(Uint8List bytes) {
  final memory = malloc<Uint8>(bytes.length);
  memory.asTypedList(bytes.length).setAll(0, bytes);
  return memory;
}

void _worker(SendPort replies) {
  final commands = ReceivePort();
  replies.send(commands.sendPort);
  late final DynamicLibrary lib;
  late final _Render render;
  late final _Size setSize;
  Pointer<Void> handle = nullptr;
  final out = malloc<Int32>(4);
  final pixels = malloc<Pointer<Uint8>>();
  commands.listen((message) {
    final m = message as List;
    switch (m[0]) {
      case 'load':
        try {
          lib = DynamicLibrary.open(m[1] as String);
          render = lib.lookupFunction<_RenderC, _Render>('otakase_ass_render');
          setSize = lib.lookupFunction<_SizeC, _Size>('otakase_ass_set_size');
          handle = lib.lookupFunction<_NewC, _NewC>('otakase_ass_new')();
          if (handle == nullptr) throw StateError('libass init failed');
          final font = (m[2] as TransferableTypedData)
              .materialize()
              .asUint8List();
          final text = (m[3] as TransferableTypedData)
              .materialize()
              .asUint8List();
          final fontData = _copy(font),
              name = 'NotoSansJP-Regular.otf'.toNativeUtf8();
          lib.lookupFunction<_AddFontC, _AddFont>('otakase_ass_add_font')(
            handle,
            name,
            fontData,
            font.length,
          );
          malloc
            ..free(fontData)
            ..free(name);
          final family = 'Noto Sans JP'.toNativeUtf8();
          lib.lookupFunction<_DefaultFontC, _DefaultFont>(
            'otakase_ass_set_default_font',
          )(handle, family);
          malloc.free(family);
          final data = _copy(text);
          final loaded = lib.lookupFunction<_LoadC, _Load>('otakase_ass_load')(
            handle,
            data,
            text.length,
          );
          malloc.free(data);
          replies.send(loaded == 0 ? 'ok' : 'error');
        } catch (_) {
          replies.send('error');
        }
      case 'render':
        final w = m[2] as int, h = m[3] as int;
        setSize(handle, w, h);
        final status = render(
          handle,
          m[1] as int,
          out,
          out + 1,
          out + 2,
          out + 3,
          pixels,
        );
        if (status == 1) {
          final bytes = pixels.value.asTypedList(out[2] * out[3] * 4);
          replies.send([
            1,
            w,
            h,
            out[0],
            out[1],
            out[2],
            out[3],
            TransferableTypedData.fromList([bytes]),
          ]);
        } else {
          replies.send([status, w, h]);
        }
      case 'close':
        if (handle != nullptr) {
          lib.lookupFunction<_FreeC, _Free>('otakase_ass_free')(handle);
        }
        malloc
          ..free(out)
          ..free(pixels);
        commands.close();
    }
  });
}
