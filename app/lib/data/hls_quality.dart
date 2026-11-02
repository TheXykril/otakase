import 'dart:convert';

import 'package:http/http.dart' as http;

/// One picture size an HLS master playlist offers.
typedef HlsVariant = ({int height, int bandwidth});

/// The variants in an HLS master playlist, smallest first.
List<HlsVariant> hlsVariants(String playlist) {
  final choices = <HlsVariant>[];
  for (final line in playlist.split('\n')) {
    if (!line.startsWith('#EXT-X-STREAM-INF:')) continue;
    final resolution = RegExp(r'RESOLUTION=\d+x(\d+)').firstMatch(line);
    final bandwidth = RegExp(r'(?:^|[,:])BANDWIDTH=(\d+)').firstMatch(line);
    final h = int.tryParse(resolution?.group(1) ?? ''),
        b = int.tryParse(bandwidth?.group(1) ?? '');
    if (h != null && b != null) choices.add((height: h, bandwidth: b));
  }
  choices.sort((a, b) => a.height.compareTo(b.height));
  return choices;
}

/// The heights to offer, largest first.
List<int> hlsHeights(List<HlsVariant> variants) =>
    variants.map((c) => c.height).toSet().toList()
      ..sort((a, b) => b.compareTo(a));

/// mpv's hls-bitrate for the Quality setting ("best", "worst" or a height
/// like "720"): the largest variant that fits, else the smallest.
String hlsBitrate(String quality, List<HlsVariant> variants) {
  if (quality == 'worst') return 'min';
  final height = int.tryParse(quality.replaceAll('p', ''));
  if (height == null || variants.isEmpty) return 'max';
  final fitting = variants.where((c) => c.height <= height).toList();
  return '${(fitting.isEmpty ? variants.first : fitting.last).bandwidth}';
}

/// Reads the master playlist at [url] (at most 1 MiB, 8 s) and returns its
/// variants; empty when it cannot be read or is not a master playlist.
Future<List<HlsVariant>> fetchHlsVariants(
  http.Client client,
  String url,
  Map<String, String> headers,
) async {
  try {
    final request = http.Request('GET', Uri.parse(url))
      ..headers.addAll(headers);
    final response = await client
        .send(request)
        .timeout(const Duration(seconds: 8));
    var length = 0;
    final data = <int>[];
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 8),
    )) {
      length += chunk.length;
      if (length > 1024 * 1024) break;
      data.addAll(chunk);
    }
    return hlsVariants(utf8.decode(data, allowMalformed: true));
  } catch (_) {
    return [];
  }
}
