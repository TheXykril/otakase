import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/playback/subtitle_cues.dart';

void main() {
  test('ASS dialogue keeps text and drops styling, drawings and comments', () {
    final cues = SubtitleCues.parse('''
[Script Info]
Title: Sample

[V4+ Styles]
Format: Name, Fontname
Style: Default,Arial

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Comment: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,hidden
Dialogue: 0,0:00:01.50,0:00:04.00,Default,,0,0,0,,{\\i1}Hello,\\Nworld{\\i0}
Dialogue: 0,0:00:02.00,0:00:03.00,Sign,,0,0,0,,{\\p1}m 0 0 l 10 10{\\p0}
Dialogue: 0,0:01:00.25,0:01:02.00,Default,,0,0,0,,Late\\hline
''');
    expect(cues.at(const Duration(milliseconds: 1000)), '');
    expect(cues.at(const Duration(milliseconds: 2500)), 'Hello,\nworld');
    expect(cues.at(const Duration(seconds: 4)), '');
    expect(cues.at(const Duration(seconds: 61)), 'Late line');
  });

  test('WebVTT and SubRip cues are read with tags removed', () {
    final vtt = SubtitleCues.parse('''
WEBVTT

00:01.000 --> 00:02.500
<b>Bold</b> &amp; plain

1:00:00.000 --> 1:00:01.000
Hour
''');
    expect(vtt.at(const Duration(milliseconds: 1500)), 'Bold & plain');
    expect(vtt.at(const Duration(hours: 1, milliseconds: 500)), 'Hour');

    final srt = SubtitleCues.parse(
      '1\r\n00:00:05,000 --> 00:00:06,000\r\nFirst\r\nline two\r\n\r\n'
      '2\r\n00:00:05,500 --> 00:00:07,000\r\nOverlap\r\n',
    );
    expect(
      srt.at(const Duration(milliseconds: 5600)),
      'First\nline two\nOverlap',
    );
    expect(srt.at(const Duration(milliseconds: 6500)), 'Overlap');
  });
}
