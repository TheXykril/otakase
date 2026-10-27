import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// Synthetic HLS fixture. Every manifest and segment requires all three
/// headers. Counts prove headers reached child playlists and media requests.
class HLSProbe {
  static const headers = {
    'X-Otakase-Probe': 'mobile-spike',
    'Referer': 'https://otakase.invalid/',
    'User-Agent': 'OtakaseSpike/1',
  };
  final accepted = ValueNotifier<int>(0);
  final rejected = ValueNotifier<int>(0);
  final resources = <String>{};
  HttpServer? _server;
  String get url => 'http://127.0.0.1:${_server!.port}/master.m3u8';

  Future<void> start() async {
    _server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    _server!.listen((request) => unawaited(_serve(request)));
  }

  Future<void> _serve(HttpRequest request) async {
    final name = request.uri.path.substring(1);
    try {
      if (headers.entries.any(
        (entry) => request.headers.value(entry.key) != entry.value,
      )) {
        rejected.value++;
        request.response.statusCode = HttpStatus.forbidden;
        return;
      }
      if (!RegExp(r'^(master\.m3u8|variant\.m3u8|segment\d{2}\.ts)$')
          .hasMatch(name)) {
        request.response.statusCode = HttpStatus.notFound;
        return;
      }
      final asset = await rootBundle.load('assets/probe/$name');
      request.response.headers.contentType = ContentType(
        name.endsWith('.ts') ? 'video' : 'application',
        name.endsWith('.ts') ? 'mp2t' : 'vnd.apple.mpegurl',
      );
      request.response.add(
        asset.buffer.asUint8List(asset.offsetInBytes, asset.lengthInBytes),
      );
      resources.add(name);
      accepted.value++;
    } catch (_) {
      request.response.statusCode = HttpStatus.internalServerError;
    } finally {
      await request.response.close();
    }
  }

  Future<void> close() async {
    await _server?.close(force: true);
    // Notifiers stay alive until pending request handlers have unwound.
  }
}
