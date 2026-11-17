import '../data/movies.dart';

/// Language names for the codes players and files usually carry.
const _languageCodes = <String, String>{
  'en': 'English',
  'eng': 'English',
  'ja': 'Japanese',
  'jpn': 'Japanese',
  'lt': 'Lithuanian',
  'lit': 'Lithuanian',
  'zh': 'Chinese',
  'chi': 'Chinese',
  'zho': 'Chinese',
  'ru': 'Russian',
  'rus': 'Russian',
  'es': 'Spanish',
  'spa': 'Spanish',
  'fr': 'French',
  'fra': 'French',
  'fre': 'French',
  'de': 'German',
  'deu': 'German',
  'ger': 'German',
  'pt': 'Portuguese',
  'por': 'Portuguese',
  'it': 'Italian',
  'ita': 'Italian',
  'ko': 'Korean',
  'kor': 'Korean',
  'ar': 'Arabic',
  'ara': 'Arabic',
};

/// A language code or name as people read it: "eng" is "English".
String languageLabel(String? language) {
  final value = (language ?? '').trim();
  if (value.isEmpty) return '';
  final code = value.toLowerCase().split(RegExp('[-_]')).first;
  return _languageCodes[code] ?? languageTitle(value);
}

/// The name of a track in the Subtitles & audio menu. [kind] names an
/// unlabelled track ("Audio 2"); [fallback] is the language a lone
/// unlabelled track is known to be in, such as an anime dub's English.
String trackLabel(
  String id,
  String? title,
  String? language, {
  required String kind,
  String? fallback,
}) {
  if (id == 'auto') return 'Automatic';
  if (id == 'no') return 'Off';
  final name = (title ?? '').trim();
  final lang = languageLabel(language);
  if (name.isNotEmpty) {
    final same =
        lang.isEmpty ||
        name.toLowerCase() == lang.toLowerCase() ||
        name.toLowerCase() == (language ?? '').trim().toLowerCase() ||
        name.toLowerCase().contains(lang.toLowerCase());
    return same ? name : '$name · $lang';
  }
  if (lang.isNotEmpty) return lang;
  if (fallback != null && fallback.isNotEmpty) return languageLabel(fallback);
  return id.isEmpty ? kind : '$kind $id';
}

/// [labels] made tellable apart: a name that appears again gets a number,
/// "English", "English 2".
List<String> distinctLabels(List<String> labels) {
  final seen = <String, int>{};
  return [
    for (final label in labels)
      switch (seen[label] = (seen[label] ?? 0) + 1) {
        1 => label,
        final n => '$label $n',
      },
  ];
}

/// A quality choice as people read it: "best" is "Best available", "1080"
/// is "1080p".
String qualityLabel(String value) => switch (value) {
  'best' => 'Best available',
  'worst' => 'Lowest',
  _ when int.tryParse(value) != null => '${value}p',
  _ => value,
};

/// Lets the viewer pick one of [values] by its [qualityLabel]; returns the
/// value, not the label.
Future<String?> chooseQuality(
  Future<String?> Function(List<String> labels, String? selected) choose,
  List<String> values,
  String? selected,
) async {
  final labels = {for (final v in values) qualityLabel(v): v};
  final picked = await choose(
    labels.keys.toList(),
    selected == null ? null : qualityLabel(selected),
  );
  return picked == null ? null : labels[picked];
}
