import 'dart:convert';

import '../core.dart';
import 'models.dart';

Iterable<Json> externalSubtitleTracks(ResolvedStream stream) sync* {
  yield* stream.subtitles;
  final url = stream.subtitle;
  if (url.isNotEmpty && !stream.subtitles.any((track) => track['url'] == url)) {
    yield {'url': url, 'label': 'Subtitles'};
  }
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
