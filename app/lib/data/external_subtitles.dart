import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

import '../core.dart';
import 'models.dart';

Iterable<Json> externalSubtitleTracks(ResolvedStream stream) sync* {
  yield* stream.subtitles;
  final url = stream.subtitle;
  if (url.isNotEmpty && !stream.subtitles.any((track) => track['url'] == url)) {
    yield {'url': url, 'label': 'Subtitles'};
  }
}

/// Prefer dialogue over closed captions and signs-only/forced tracks, retaining
/// source order for equal choices. Apply within one preferred language/copy.
Iterable<T> dialogueSubtitleOrder<T>(
  Iterable<T> tracks, {
  required String? Function(T) language,
  required String? Function(T) label,
}) {
  int rank(T track) {
    final text = '${language(track) ?? ''} ${label(track) ?? ''}'.toLowerCase();
    if (RegExp(r'\b(forced|signs?|songs?)\b').hasMatch(text)) return 2;
    if (RegExp(r'\b(cc|sdh|closed captions?|hearing impaired)\b')
        .hasMatch(text)) {
      return 1;
    }
    return 0;
  }

  final indexed = tracks.indexed.toList();
  indexed.sort((a, b) {
    final order = rank(a.$2).compareTo(rank(b.$2));
    return order != 0 ? order : a.$1.compareTo(b.$1);
  });
  return indexed.map((entry) => entry.$2);
}

/// Give native demuxers a filename hint while retaining the original bytes.
String subtitleExtension(List<int> bytes, Uri uri) {
  final prefix = utf8
      .decode(bytes.take(512).toList(), allowMalformed: true)
      .replaceFirst('\uFEFF', '')
      .trimLeft();
  if (prefix.isEmpty || prefix.startsWith('<')) {
    throw const FormatException('Expected a subtitle file');
  }
  if (prefix.startsWith('WEBVTT')) return 'vtt';
  if (prefix.toLowerCase().startsWith('[script info]')) return 'ass';
  if (RegExp(r'^\d+\s+\d\d:\d\d:\d\d[,\.]\d+\s+-->').hasMatch(prefix)) {
    return 'srt';
  }
  final extension = uri.path.split('.').last.toLowerCase();
  return const {'vtt', 'srt', 'ass', 'ssa'}.contains(extension)
      ? extension
      : 'sub';
}

class SubtitleFailure implements Exception {
  const SubtitleFailure(this.message, {this.host});
  final String message;

  /// Only failures reaching the sidecar trigger an automatic source change.
  final String? host;
}

Future<({List<int> bytes, String extension})> downloadSubtitle(
  http.Client client,
  ResolvedStream stream,
  String link, {
  CoreClient? nativeCore,
  RequestScope? scope,
  Duration timeout = const Duration(seconds: 15),
}) async {
  final url = Uri.tryParse(link.trim());
  final video = Uri.tryParse(stream.url);
  final referrer = Uri.tryParse(stream.referrer);
  final base = video != null && !{'127.0.0.1', 'localhost'}.contains(video.host)
      ? video
      : referrer;
  final uri = url == null || link.trim().isEmpty
      ? null
      : base?.resolveUri(url) ?? url;
  if (uri == null ||
      !{'http', 'https'}.contains(uri.scheme) ||
      uri.host.isEmpty) {
    throw const SubtitleFailure('The subtitle link is invalid.');
  }

  final headers = Map<String, String>.of(stream.headers);
  if (stream.referrer.isNotEmpty) headers['Referer'] = stream.referrer;
  final List<int> bytes;
  if (nativeCore == null) {
    bytes = await downloadSubtitleWithHttp(
      client,
      uri,
      headers,
      timeout: timeout,
    );
  } else {
    try {
      final data = await nativeCore.request(
        {
          'operation': 'subtitle_download',
          'subtitle': {'url': uri.toString(), 'headers': headers},
        },
        scope: scope,
        timeout: timeout,
      );
      bytes = base64Decode(data['body'] as String? ?? '');
    } on CoreFailure catch (e) {
      throw SubtitleFailure(
        e.message,
        host:
            e.code.startsWith('subtitle_tls') ||
                const {
                  'subtitle_dns',
                  'subtitle_network',
                  'subtitle_timeout',
                  'subtitle_http',
                  'timeout',
                }.contains(e.code)
            ? uri.host
            : null,
      );
    }
  }
  try {
    return (bytes: bytes, extension: subtitleExtension(bytes, uri));
  } on FormatException {
    throw const SubtitleFailure(
      'The subtitle server returned an invalid file.',
    );
  }
}

Future<List<int>> downloadSubtitleWithHttp(
  http.Client client,
  Uri uri,
  Map<String, String> headers, {
  required Duration timeout,
}) async {
  final http.Response response;
  try {
    response = await client.get(uri, headers: headers).timeout(timeout);
  } on TimeoutException {
    throw SubtitleFailure('The subtitle download timed out.', host: uri.host);
  } on Exception {
    throw SubtitleFailure(
      'The subtitle server could not be reached.',
      host: uri.host,
    );
  }
  if (response.statusCode != 200) {
    throw SubtitleFailure(
      'Subtitle download failed (HTTP ${response.statusCode}).',
      host: uri.host,
    );
  }
  return response.bodyBytes;
}

/// mpv's requested subtitle setting can be automatic or reuse the same ID.
/// Read back the source of the active track to confirm which file opened.
bool isActiveSubtitleFile(File file, String selected) =>
    selected == file.path || selected == file.uri.toString();

Future<void> deleteSubtitleDirectory(Directory directory) async {
  try {
    await directory.delete(recursive: true);
  } on FileSystemException {
    // The OS can also clear the app cache.
  }
}
