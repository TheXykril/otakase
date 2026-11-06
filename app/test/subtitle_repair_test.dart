import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/external_subtitles.dart';
import 'package:otakase/data/models.dart';

// Synthetic equivalent of the malformed blocks observed in live episode files.
const broken =
    'WEBVTT\n\n'
    '02:58.650 --> 03:01.660\nFirst cue\n\n\n'
    'Orphan text\n\nAnother orphan\n\n'
    'cue-id\n03:02.020 --> 03:05.100 align:start\nLater dialogue\n\n'
    '23:47.000 --> 23:49.990\nFinal dialogue\n';

void main() {
  test(
    'orphan blocks are removed without losing later cues or cue settings',
    () {
      final repaired = utf8.decode(repairWebVtt(utf8.encode(broken)));
      expect(repaired, isNot(contains('Orphan text')));
      expect(repaired, isNot(contains('Another orphan')));
      expect(repaired, contains('cue-id\n03:02.020 --> 03:05.100 align:start'));
      expect(repaired, contains('Final dialogue'));
      expect('-->'.allMatches(repaired).length, 3);
      expect(repairWebVtt(utf8.encode(repaired)), utf8.encode(repaired));
    },
  );

  test('valid BOM/CRLF files, metadata and cue text retain their bytes', () {
    final bytes = utf8.encode(
      '\uFEFFWEBVTT\r\n\r\n'
      'NOTE explanation\r\nComment\r\n\r\n'
      'STYLE\r\n::cue { color: white; }\r\n\r\n'
      'REGION\r\nid:main\r\n\r\n'
      'identifier\r\n00:00:01.000 --> 00:00:02.000\r\n'
      '<i>Text</i>\r\nSecond line\r\n',
    );
    expect(identical(repairWebVtt(bytes), bytes), isTrue);
  });

  test('repair handles whitespace separators and retains metadata', () {
    final bytes = utf8.encode(
      'WEBVTT\n\nNOTE keep this\n\n'
      'Bad block\n \t\nSTYLE\n::cue { color: white; }\n\n'
      '00:01.000 --> 00:02.000\nText\n',
    );
    final result = utf8.decode(repairWebVtt(bytes));
    expect(result, isNot(contains('Bad block')));
    expect(result, contains('NOTE keep this'));
    expect(result, contains('STYLE\n::cue'));
    expect(result, contains('00:01.000 --> 00:02.000'));
  });

  for (final native in [false, true]) {
    test(
      'download repairs WebVTT over ${native ? 'native' : 'HTTP'} transport',
      () async {
        final client = MockClient((_) async => http.Response(broken, 200));
        addTearDown(client.close);
        final result = await downloadSubtitle(
          client,
          ResolvedStream.fromJson({'url': 'https://video.example/video.m3u8'}),
          'https://subtitles.example/dialogue.vtt',
          nativeCore: native ? _SubtitleCore() : null,
        );
        expect(result.extension, 'vtt');
        expect(utf8.decode(result.bytes), contains('Final dialogue'));
        expect(utf8.decode(result.bytes), isNot(contains('Orphan text')));
      },
    );
  }
}

class _SubtitleCore extends CoreClient {
  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async => {'body': base64Encode(utf8.encode(broken))};
}
