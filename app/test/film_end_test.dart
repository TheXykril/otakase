import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/play_request.dart';

void main() {
  const film = Duration(minutes: 101);

  test('a stream that stops early is not the end of the film', () {
    expect(filmEnded(const Duration(minutes: 6), film, 101 * 60), isFalse);
    expect(filmEnded(const Duration(minutes: 95), film, 101 * 60), isFalse);
  });

  test('the last seconds of the film are the end', () {
    expect(
      filmEnded(film - const Duration(seconds: 2), film, 101 * 60),
      isTrue,
    );
    expect(filmEnded(film, film, 0), isTrue);
  });

  test('a copy far shorter than the film never ends it', () {
    const cut = Duration(minutes: 6);
    expect(copyLengthTrusted(cut, 101 * 60), isFalse);
    expect(filmEnded(cut, cut, 101 * 60), isFalse);
  });

  test('an unknown or slightly different running time is trusted', () {
    expect(copyLengthTrusted(const Duration(minutes: 6), 0), isTrue);
    expect(copyLengthTrusted(const Duration(minutes: 94), 101 * 60), isTrue);
  });

  test('nothing ends before the length is known', () {
    expect(filmEnded(Duration.zero, Duration.zero, 0), isFalse);
    expect(filmEnded(const Duration(minutes: 6), Duration.zero, 0), isFalse);
  });
}
