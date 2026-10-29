import 'dart:async';
import 'dart:math';

final syncedResumeTagPattern = RegExp(r'\[otakase:(\d+)@(\d+|done)\]');
final _tagSpan = RegExp(r'\[otakase:\d+@(?:\d+\]|done\][^\n]*)');

class SyncedResume {
  const SyncedResume(this.episode, this.seconds);
  final int episode, seconds;
  bool get valid => episode > 0 && seconds > 0;
  int forEpisode(int value) => episode == value ? seconds : 0;
  @override
  bool operator ==(Object other) =>
      other is SyncedResume &&
      episode == other.episode &&
      seconds == other.seconds;
  @override
  int get hashCode => Object.hash(episode, seconds);
}

const noResume = SyncedResume(0, 0);
String formatSyncedResumeTag(SyncedResume p) =>
    '[otakase:${p.episode}@${p.seconds}]';
SyncedResume? parseSyncedResumeTag(String notes) {
  final match = syncedResumeTagPattern.firstMatch(notes);
  if (match == null) return null;
  final p = SyncedResume(
    int.tryParse(match[1]!) ?? 0,
    int.tryParse(match[2]!) ?? 0,
  );
  return p.valid ? p : null;
}

String withSyncedTag(String notes, String tag) {
  if (_tagSpan.hasMatch(notes)) {
    var replaced = false;
    return notes.replaceAllMapped(_tagSpan, (_) {
      if (replaced) return '';
      replaced = true;
      return tag;
    });
  }
  final trimmed = notes.replaceFirst(RegExp(r'[ \t\r\n]+$'), '');
  return trimmed.isEmpty ? tag : '$trimmed\n$tag';
}

String withSyncedResumeTag(String notes, SyncedResume p) =>
    withSyncedTag(notes, formatSyncedResumeTag(p));
String syncedResumeAfterEpisode(
  String notes,
  int finished,
  int total, {
  String Function()? doneLine,
}) {
  final match = syncedResumeTagPattern.firstMatch(notes);
  if (match != null && (int.tryParse(match[1]!) ?? 0) > finished) return notes;
  if (total > 0 && finished >= total) {
    final line =
        (doneLine ??
        () => syncedDoneLines[Random().nextInt(syncedDoneLines.length)])();
    return withSyncedTag(notes, '[otakase:$finished@done] $line');
  }
  return withSyncedResumeTag(notes, SyncedResume(finished + 1, 0));
}

SyncedResume laterSyncedResume(SyncedResume a, SyncedResume b) {
  if (!a.valid) return b;
  if (!b.valid) return a;
  if (a.episode != b.episode) return a.episode > b.episode ? a : b;
  return a.seconds >= b.seconds ? a : b;
}

bool syncedResumeWorthPushing(SyncedResume pushed, SyncedResume next) =>
    next.valid &&
    next.seconds >= 10 &&
    (pushed.episode != next.episode ||
        (next.seconds - pushed.seconds).abs() >= 5);

class ResumePusher {
  ResumePusher({
    required this.push,
    required this.finish,
    DateTime Function()? now,
    this.onError,
  }) : now = now ?? DateTime.now;
  final Future<void> Function(String, SyncedResume) push;
  final Future<void> Function(String, int, int) finish;
  final DateTime Function() now;
  final void Function(Object)? onError;
  String _show = '', _doneShow = '';
  int _doneEp = 0;
  SyncedResume _pending = noResume, _pushed = noResume;
  DateTime? _lastPush;
  bool _busy = false;
  Future<void> _writing = Future.value();
  Future<void> get idle => _writing;
  bool _stale(String show, SyncedResume p) =>
      show == _doneShow && p.episode <= _doneEp;
  void note(String show, int episode, int seconds) {
    final next = SyncedResume(episode, seconds);
    if (show.isEmpty || !next.valid || seconds < 10 || _stale(show, next)) {
      return;
    }
    final time = now();
    if (show != _show) {
      if (_show.isNotEmpty && syncedResumeWorthPushing(_pushed, _pending)) {
        _run(_show, _pending, false);
      }
      _show = show;
      _pushed = noResume;
      _lastPush = time;
    }
    _lastPush ??= time;
    _pending = next;
    if (_busy ||
        time.difference(_lastPush!) < const Duration(seconds: 60) ||
        !syncedResumeWorthPushing(_pushed, next)) {
      return;
    }
    _busy = true;
    _lastPush = time;
    _run(show, next, true);
  }

  Future<void> _run(String show, SyncedResume p, bool paced) {
    return _writing = _writing.then((_) async {
      try {
        if (!_stale(show, p)) {
          await push(show, p);
          if (_show == show) _pushed = p;
        }
      } catch (e) {
        onError?.call(e);
      } finally {
        if (paced) _busy = false;
      }
    });
  }

  Future<void> episodeDone(String show, int finished, int total) {
    if (show.isEmpty || finished < 1) return Future.value();
    if (_doneShow != show) {
      _doneShow = show;
      _doneEp = 0;
    }
    _doneEp = max(_doneEp, finished);
    if (_show == show && _stale(show, _pending)) _pending = noResume;
    return _writing = _writing.then((_) async {
      try {
        await finish(show, finished, total);
      } catch (e) {
        onError?.call(e);
      }
    });
  }

  Future<void> flush() async {
    if (_show.isNotEmpty && syncedResumeWorthPushing(_pushed, _pending)) {
      await _run(_show, _pending, false);
    }
    await _writing;
  }
}

const syncedDoneLines = <String>[
  "お任せ. Otakase saw it through.",
  "From first frame to last, お任せ.",
  "お任せ. Another journey entrusted.",
  "Otakase, with care until the finale.",
  "Your story was in our hands. お任せ.",
  "任. Another story entrusted.",
  "Chosen by Otakase. Sealed with 任.",
  "任. The final frame bears our seal.",
  "One story. One seal. Otakase.",
  "任. A journey signed by Otakase.",
  "余韻. Let the feeling linger.",
  "Otakase leaves you with 余韻.",
  "The credits fade. The 余韻 stays.",
  "余韻. Our final gift from Otakase.",
  "余韻. Let the feeling linger.",
  "Otakase leaves you with 余韻.",
  "The credits fade. The 余韻 stays.",
  "余韻. Our final gift from Otakase.",
  "一期一会. Worth every episode.",
  "一期一会. Otakase brought us here.",
  "One encounter, one story. 一期一会.",
  "Otakase treasures this 一期一会.",
  "物語. Carefully chosen by Otakase.",
  "One 物語, a thousand memories.",
  "Otakase's seal upon this 物語.",
  "物語. From our hands to your heart.",
  "またね. Otakase will be here.",
  "Otakase says またね, never goodbye.",
  "またね. The next story is on us.",
  "From Otakase, a quiet またね.",
  "またね. Otakase will be here.",
  "Otakase says またね, never goodbye.",
  "またね. The next story is on us.",
  "From Otakase, a quiet またね.",
  "完. Otakase seals the final frame.",
  "完. A story entrusted, completed.",
  "完. Signed and sealed by Otakase.",
  "完. Another Otakase journey fulfilled.",
];
