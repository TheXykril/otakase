import 'dart:ffi';
import 'dart:isolate';

import 'package:ffi/ffi.dart';
import 'package:flutter/services.dart';

typedef _StartC = Int64 Function(Pointer<Utf8>);
typedef _Start = int Function(Pointer<Utf8>);
typedef _ResultC = Pointer<Utf8> Function(Int64);
typedef _Result = Pointer<Utf8> Function(int);
typedef _CancelC = Void Function(Int64);
typedef _Cancel = void Function(int);
typedef _FreeC = Void Function(Pointer<Utf8>);
typedef _Free = void Function(Pointer<Utf8>);
typedef _PathC = Pointer<Utf8> Function();

/// The Go core on Samsung TVs: libotakasecore.so (mobilecore/clib) loaded
/// with dart:ffi. It answers the same JSON requests as the Android binding.
class TizenCore {
  static final DynamicLibrary _library = _open();
  static final _start = _library.lookupFunction<_StartC, _Start>(
    'otakase_core_start',
  );
  static final _cancel = _library.lookupFunction<_CancelC, _Cancel>(
    'otakase_core_cancel',
  );

  static DynamicLibrary _open() {
    try {
      return DynamicLibrary.open('libotakasecore.so');
    } on ArgumentError {
      // Tizen keeps app libraries in <package>/lib, next to res/.
      final common = DynamicLibrary.open('libcapi-appfw-app-common.so.0');
      final resource = common
          .lookupFunction<_PathC, _PathC>('app_get_resource_path')();
      final path = resource.toDartString();
      malloc.free(resource);
      final root = path.replaceFirst(RegExp(r'res/?$'), '');
      return DynamicLibrary.open('${root}lib/libotakasecore.so');
    }
  }

  static Future<int> start(String input) async {
    final text = input.toNativeUtf8();
    try {
      final id = _start(text);
      if (id == 0) {
        throw PlatformException(code: 'busy', message: 'Core closed or busy');
      }
      return id;
    } finally {
      malloc.free(text);
    }
  }

  /// Waits on a worker isolate: the Go call blocks until the request ends.
  static Future<String> result(int id) => Isolate.run(() {
    final library = _open();
    final result = library.lookupFunction<_ResultC, _Result>(
      'otakase_core_result',
    );
    final free = library.lookupFunction<_FreeC, _Free>('otakase_core_free');
    final value = result(id);
    try {
      return value.toDartString();
    } finally {
      free(value);
    }
  });

  static Future<void> cancel(int id) async => _cancel(id);
}
