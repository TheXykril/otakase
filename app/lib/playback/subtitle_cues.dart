/// Plain-text subtitle cues for players without libass (Samsung TVs).
/// ASS styling and positions are dropped; the dialogue text is kept.
class SubtitleCue {
  const SubtitleCue(this.start, this.end, this.text);
  final Duration start, end;
  final String text;
}

class SubtitleCues {
  SubtitleCues(List<SubtitleCue> cues)
    : cues = List.of(cues)..sort((a, b) => a.start.compareTo(b.start));
  final List<SubtitleCue> cues;

  /// Parses ASS/SSA, WebVTT or SubRip text.
  factory SubtitleCues.parse(String text) {
    final body = text.replaceAll('\r\n', '\n').replaceAll('\r', '\n');
    return SubtitleCues(
      RegExp(r'^\s*\[Events\]', multiLine: true).hasMatch(body)
          ? _ass(body)
          : _timed(body),
    );
  }

  /// The text on screen at [position], or an empty string.
  String at(Duration position) {
    final lines = <String>[];
    for (final cue in cues) {
      if (cue.start > position) break;
      if (cue.end > position && !lines.contains(cue.text)) lines.add(cue.text);
    }
    return lines.join('\n');
  }

  static List<SubtitleCue> _ass(String body) {
    final cues = <SubtitleCue>[];
    var fields = <String>[];
    var events = false;
    for (final raw in body.split('\n')) {
      final line = raw.trim();
      if (line.startsWith('[')) {
        events = line.toLowerCase() == '[events]';
        continue;
      }
      if (!events) continue;
      final colon = line.indexOf(':');
      if (colon < 0) continue;
      final kind = line.substring(0, colon).trim().toLowerCase();
      final value = line.substring(colon + 1);
      if (kind == 'format') {
        fields = value.split(',').map((f) => f.trim().toLowerCase()).toList();
        continue;
      }
      if (kind != 'dialogue' || fields.isEmpty) continue;
      final parts = value.split(',');
      if (parts.length < fields.length) continue;
      final textIndex = fields.indexOf('text');
      final startIndex = fields.indexOf('start');
      final endIndex = fields.indexOf('end');
      if (textIndex < 0 || startIndex < 0 || endIndex < 0) continue;
      final start = _clock(parts[startIndex]);
      final end = _clock(parts[endIndex]);
      final rawText = parts.sublist(textIndex).join(',');
      // \p1..\p4 draw vector shapes; they are not readable text.
      if (start == null ||
          end == null ||
          RegExp(r'\\p[1-9]').hasMatch(rawText)) {
        continue;
      }
      final text = rawText
          .replaceAll(RegExp(r'\{[^}]*\}'), '')
          .replaceAll(RegExp(r'\\[Nn]'), '\n')
          .replaceAll(r'\h', ' ')
          .trim();
      if (text.isNotEmpty) cues.add(SubtitleCue(start, end, text));
    }
    return cues;
  }

  static List<SubtitleCue> _timed(String body) {
    final cues = <SubtitleCue>[];
    final timing = RegExp(r'^\s*(\S+)\s+-->\s+(\S+)');
    final lines = body.split('\n');
    for (var i = 0; i < lines.length; i++) {
      final match = timing.firstMatch(lines[i]);
      if (match == null) continue;
      final start = _clock(match.group(1)!), end = _clock(match.group(2)!);
      final text = <String>[];
      while (i + 1 < lines.length && lines[i + 1].trim().isNotEmpty) {
        text.add(lines[++i].trim());
      }
      final value = text
          .join('\n')
          .replaceAll(RegExp(r'<[^>]*>'), '')
          .replaceAll('&amp;', '&')
          .replaceAll('&lt;', '<')
          .replaceAll('&gt;', '>')
          .replaceAll('&nbsp;', ' ')
          .trim();
      if (start != null && end != null && value.isNotEmpty) {
        cues.add(SubtitleCue(start, end, value));
      }
    }
    return cues;
  }

  /// Reads h:mm:ss.cc (ASS), hh:mm:ss.mmm / mm:ss.mmm (VTT) and
  /// hh:mm:ss,mmm (SRT).
  static Duration? _clock(String value) {
    final match = RegExp(r'^(?:(\d+):)?(\d{1,2}):(\d{1,2})(?:[.,](\d{1,3}))?$')
        .firstMatch(value.trim());
    if (match == null) return null;
    final fraction = match.group(4) ?? '0';
    return Duration(
      hours: int.parse(match.group(1) ?? '0'),
      minutes: int.parse(match.group(2)!),
      seconds: int.parse(match.group(3)!),
      milliseconds: int.parse(fraction.padRight(3, '0')),
    );
  }
}
