import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/stay_awake.dart';

void main() {
  test('screen stays on while work runs or the setting is on', () async {
    final calls = <(bool, String?)>[];
    final awake = StayAwake(
      apply: (screen, work) async {
        calls.add((screen, work));
      },
    );

    awake.busy('update', 'Downloading the Otakase update');
    expect(calls.last, (true, 'Downloading the Otakase update'));

    awake.busy('downloads', 'Saving 2 downloads');
    awake.busy('update', null);
    expect(calls.last, (true, 'Saving 2 downloads'));

    awake.busy('downloads', null);
    expect(calls.last, (false, null));

    awake.always = true;
    expect(calls.last, (true, null));
    awake.always = false;
    expect(calls.last, (false, null));
  });

  test('repeated states are not sent again', () async {
    var count = 0;
    final awake = StayAwake(apply: (_, _) async => count++);
    awake.busy('downloads', 'Saving 1 download');
    awake.busy('downloads', 'Saving 1 download');
    awake.always = true;
    expect(count, 1);
  });
}
