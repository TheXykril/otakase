import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/hls_quality.dart';

const master = '''#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360
360.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080
1080.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=2000000,BANDWIDTH=2800000,RESOLUTION=1280x720
720.m3u8
''';

void main() {
  test('reads the sizes a master playlist offers', () {
    final v = hlsVariants(master);
    expect(v.map((x) => x.height), [360, 720, 1080]);
    // AVERAGE-BANDWIDTH is not taken for BANDWIDTH.
    expect(v[1].bandwidth, 2800000);
    expect(hlsHeights(v), [1080, 720, 360]);
    expect(hlsVariants('#EXTM3U\nseg1.ts\n'), isEmpty);
  });

  test('the Quality setting picks the largest size that fits', () {
    final v = hlsVariants(master);
    expect(hlsBitrate('best', v), 'max');
    expect(hlsBitrate('worst', v), 'min');
    expect(hlsBitrate('720', v), '2800000');
    expect(hlsBitrate('900p', v), '2800000');
    expect(hlsBitrate('240', v), '800000');
    expect(hlsBitrate('720', const []), 'max');
  });
}
