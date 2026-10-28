import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/synced_resume.dart';

void main() {
  test('resume tag round trips', () {
    expect(
      formatSyncedResumeTag(const SyncedResume(5, 754)),
      '[otakase:5@754]',
    );
    expect(
      parseSyncedResumeTag('rewatch with friends\n[otakase:5@754]'),
      const SyncedResume(5, 754),
    );
  });
  test('a note without a valid tag has no resume', () {
    for (final note in [
      '',
      'great show',
      '[otakase:0@754]',
      '[otakase:5@0]',
      '[otakase:x@1]',
      '[otakase:12@done] The Voices',
    ]) {
      expect(parseSyncedResumeTag(note), isNull, reason: note);
    }
  });
  test('setting the tag keeps viewer text and removes duplicate tags', () {
    final cases = {
      '': '[otakase:6@90]',
      '   \n': '[otakase:6@90]',
      'watch the OVA after ep 12': 'watch the OVA after ep 12\n[otakase:6@90]',
      'line one\n[otakase:5@754]\nline two':
          'line one\n[otakase:6@90]\nline two',
      '[otakase:5@754] and [otakase:4@12]': '[otakase:6@90] and ',
    };
    for (final c in cases.entries) {
      expect(withSyncedResumeTag(c.key, const SyncedResume(6, 90)), c.value);
    }
  });
  test('the tracker further into the show wins', () {
    const a = SyncedResume(5, 900),
        b = SyncedResume(6, 30),
        c = SyncedResume(5, 1000);
    expect(laterSyncedResume(a, b), b);
    expect(laterSyncedResume(a, c), c);
    expect(laterSyncedResume(noResume, a), a);
    expect(laterSyncedResume(a, noResume), a);
    expect(laterSyncedResume(a, a), a);
  });
  test('resume applies only to its episode', () {
    const p = SyncedResume(5, 754);
    expect(p.forEpisode(5), 754);
    expect(p.forEpisode(6), 0);
  });
  test('positions are paced and the last position is flushed once', () async {
    var clock = DateTime.fromMillisecondsSinceEpoch(1000000);
    final writes = <SyncedResume>[];
    final r = ResumePusher(
      now: () => clock,
      push: (_, p) async {
        writes.add(p);
      },
      finish: (_, e, t) async {},
    );
    for (var sec = 10; sec <= 50; sec += 10) {
      r.note('42', 5, sec);
      clock = clock.add(const Duration(seconds: 10));
    }
    expect(writes, isEmpty);
    clock = clock.add(const Duration(seconds: 60));
    r.note('42', 5, 120);
    await r.idle;
    expect(writes, [const SyncedResume(5, 120)]);
    clock = clock.add(const Duration(seconds: 5));
    r.note('42', 5, 125);
    await r.flush();
    expect(writes.last, const SyncedResume(5, 125));
    await r.flush();
    expect(writes.length, 2);
  });
  test(
    'first polls before resume seek do not replace saved position',
    () async {
      final writes = <SyncedResume>[];
      final r = ResumePusher(
        push: (_, p) async {
          writes.add(p);
        },
        finish: (_, e, t) async {},
      );
      r.note('42', 5, 1);
      await r.flush();
      expect(writes, isEmpty);
      expect(
        syncedResumeWorthPushing(
          const SyncedResume(5, 100),
          const SyncedResume(5, 104),
        ),
        isFalse,
      );
      expect(
        syncedResumeWorthPushing(
          const SyncedResume(5, 100),
          const SyncedResume(5, 95),
        ),
        isTrue,
      );
    },
  );
  test(
    'finish moves to next episode, preserves later tags and replaces done line',
    () {
      final cases = <(String, int, int, String)>[
        ('', 5, 12, '[otakase:6@0]'),
        ('loved it', 5, 12, 'loved it\n[otakase:6@0]'),
        ('loved it\n[otakase:5@1300]', 5, 12, 'loved it\n[otakase:6@0]'),
        ('[otakase:5@1300]', 5, 0, '[otakase:6@0]'),
        (
          'loved it\n[otakase:12@1300]',
          12,
          12,
          'loved it\n[otakase:12@done] The Voices',
        ),
        (
          '[otakase:11@done] Counting the Stars',
          12,
          12,
          '[otakase:12@done] The Voices',
        ),
        ('[otakase:9@300]', 5, 12, '[otakase:9@300]'),
      ];
      for (final c in cases) {
        expect(
          syncedResumeAfterEpisode(
            c.$1,
            c.$2,
            c.$3,
            doneLine: () => 'The Voices',
          ),
          c.$4,
        );
      }
      expect(
        withSyncedResumeTag(
          '[otakase:12@done] The Voices\nmy note',
          const SyncedResume(1, 30),
        ),
        '[otakase:1@30]\nmy note',
      );
    },
  );
  test(
    'finished episode and stale queued polls are not written at exit',
    () async {
      final writes = <SyncedResume>[];
      final r = ResumePusher(
        push: (_, p) async {
          writes.add(p);
        },
        finish: (_, e, t) async {
          writes.add(SyncedResume(e + 1, 0));
        },
      );
      r.note('42', 5, 1300);
      await r.episodeDone('42', 5, 12);
      r.note('42', 5, 1310);
      await r.flush();
      expect(writes, [const SyncedResume(6, 0)]);
      r.note('42', 6, 40);
      await r.flush();
      expect(writes.last, const SyncedResume(6, 40));
    },
  );
  test('the final episode only writes its finish', () async {
    final writes = <SyncedResume>[];
    final r = ResumePusher(
      push: (_, p) async {
        writes.add(p);
      },
      finish: (_, e, t) async {
        writes.add(SyncedResume(e + 1, 0));
      },
    );
    r.note('42', 12, 1300);
    await r.episodeDone('42', 12, 12);
    await r.flush();
    expect(writes, [const SyncedResume(13, 0)]);
  });
  test('changing shows flushes the previous pending position', () async {
    final writes = <String>[];
    final r = ResumePusher(
      push: (s, p) async {
        writes.add('$s:${p.seconds}');
      },
      finish: (_, e, t) async {},
    );
    r.note('42', 5, 100);
    r.note('43', 1, 20);
    await r.flush();
    expect(writes, ['42:100', '43:20']);
  });
}
