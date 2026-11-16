import 'package:media_kit/media_kit.dart';

import '../data/movies.dart';
import '../data/play_request.dart';

/// An audio language another copy of the same episode or title plays in:
/// another source's film or series copy, or an anime episode's dub (or
/// sub). Picking it opens that copy at the same spot.
class OtherAudio {
  const OtherAudio({required this.language, this.mode = '', this.provider});

  /// The language, as settings name it ("lithuanian", "english").
  final String language;

  /// For an anime episode, the copy's mode ("dub" or "sub"); empty for a
  /// film or series.
  final String mode;

  /// For an anime episode, the source found carrying that mode, tried first.
  final String? provider;

  String get label => '${languageTitle(language)} (other copy)';
}

/// The language an anime copy in [mode] plays in: a dub is English, a sub
/// keeps the original Japanese.
String animeAudioLanguage(String mode) =>
    mode == 'dub' ? 'english' : 'japanese';

/// The other copies worth offering: those whose language no audio track of
/// the playing copy already has, each language once.
List<OtherAudio> offeredAudio(List<OtherAudio> found, List<AudioTrack> tracks) {
  final seen = <String>{};
  return [
    for (final other in found)
      if (seen.add(other.language) &&
          !tracks.any(
            (t) =>
                t.id != 'auto' &&
                t.id != 'no' &&
                languageMatches(t.language, t.title, other.language),
          ))
        other,
  ];
}
