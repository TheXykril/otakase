import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/downloads.dart';
import 'package:otakase/ui/downloads_page.dart';

void main() {
  test('failed downloads read in plain words', () {
    expect(
      downloadErrorText('the server answered 429'),
      'The source is busy. Try again in a few minutes.',
    );
    expect(downloadErrorText('the server answered 503'), contains('trouble'));
    expect(
      downloadErrorText('the server answered 404'),
      isNot(contains('404')),
    );
    expect(downloadErrorText(''), 'Failed');
    expect(
      downloadErrorText('Stopped when the app closed.'),
      'Stopped when the app closed.',
    );
  });

  test('only downloads that know what they saved offer Retry', () {
    DownloadItem item(Map<String, dynamic> meta) => DownloadItem(
      id: 'x',
      kind: 'anime',
      title: 'Story',
      label: 'Episode 1',
      base: '/tmp/Story',
      state: 'failed',
      meta: meta,
    );
    expect(canRetryDownload(item(const {})), isFalse);
    expect(
      canRetryDownload(
        item({
          'anime': {'id': 1, 'title': 'Story'},
          'episode': 1,
        }),
      ),
      isTrue,
    );
    expect(
      canRetryDownload(
        item({
          'title': {'kind': 'movie', 'trakt': 2, 'title': 'Film'},
        }),
      ),
      isTrue,
    );
  });
}
