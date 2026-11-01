import 'dart:ffi';
import 'dart:isolate';

import 'package:ffi/ffi.dart';
import 'package:flutter/services.dart';

import 'platform.dart';

typedef _StartC = Int64 Function(Pointer<Utf8>);
typedef _Start = int Function(Pointer<Utf8>);
typedef _ResultC = Pointer<Utf8> Function(Int64);
typedef _Result = Pointer<Utf8> Function(int);
typedef _CallC = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _CancelC = Void Function(Int64);
typedef _Cancel = void Function(int);
typedef _FreeC = Void Function(Pointer<Utf8>);
typedef _Free = void Function(Pointer<Utf8>);
typedef _PathC = Pointer<Utf8> Function();

/// Tizen app paths from libcapi-appfw-app-common, as returned by
/// app_get_resource_path() and app_get_data_path() (both end with "/").
class TizenPaths {
  static final _common = DynamicLibrary.open('libcapi-appfw-app-common.so.0');
  static String _call(String name) {
    final value = _common.lookupFunction<_PathC, _PathC>(name)();
    try {
      return value.toDartString();
    } finally {
      malloc.free(value);
    }
  }

  static final String resource = _call('app_get_resource_path');
  static final String data = _call('app_get_data_path');
  static String get package => resource.replaceFirst(RegExp(r'res/?$'), '');
}

/// A native library shipped with the running Dart code: an installed update
/// brings its own copies, otherwise they are in the package's lib/.
String tizenLibrary(String name) =>
    tizenBrain != null ? '$tizenBrain/$name' : '${TizenPaths.package}lib/$name';

/// The Go core on Samsung TVs: libotakasecore.so (mobilecore/clib) loaded
/// with dart:ffi. It answers the same JSON requests as the Android binding.
/// An installed update brings its own copy, next to its Dart code.
class TizenCore {
  static final String path = tizenLibrary('libotakasecore.so');
  static final DynamicLibrary _library = DynamicLibrary.open(path);
  static final _start = _library.lookupFunction<_StartC, _Start>(
    'otakase_core_start',
  );
  static final _cancel = _library.lookupFunction<_CancelC, _Cancel>(
    'otakase_core_cancel',
  );

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
  static Future<String> result(int id) {
    final library = path;
    return Isolate.run(() {
      final core = DynamicLibrary.open(library);
      final value = core.lookupFunction<_ResultC, _Result>(
        'otakase_core_result',
      )(id);
      try {
        return value.toDartString();
      } finally {
        core.lookupFunction<_FreeC, _Free>('otakase_core_free')(value);
      }
    });
  }

  static Future<void> cancel(int id) async => _cancel(id);

  /// Runs otakase_tv_update (package tvupdate) on a worker isolate.
  static Future<String> tvUpdate(String input) {
    final library = path;
    return Isolate.run(() {
      final core = DynamicLibrary.open(library);
      final text = input.toNativeUtf8();
      final value = core.lookupFunction<_CallC, _CallC>('otakase_tv_update')(
        text,
      );
      malloc.free(text);
      try {
        return value.toDartString();
      } finally {
        core.lookupFunction<_FreeC, _Free>('otakase_core_free')(value);
      }
    });
  }
}
