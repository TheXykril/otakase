import 'package:otakase/core.dart';
import 'package:otakase/data/models.dart';

/// Stands in for the Go core in the website demo: one made-up source whose
/// every episode is demo.mp4, a generated clip with no show in it.
class DemoCore extends CoreClient {
  DemoCore(this.video, this.episodes);
  final Uri video;
  final int Function(String showId) episodes;

  @override
  Future<bool> isTV() async => false;

  @override
  Future<bool> metered() async => false;

  @override
  Future<void> cancel(int id) async {}

  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async {
    scope?.check();
    switch (input['operation']) {
      case 'providers':
        return {
          'providers': ['demo'],
          'adult': <String>[],
        };
      case 'match':
        return {
          'show': {'id': '${input['anilist_id']}'},
        };
      case 'resolve':
        return {
          'streams': [
            {'url': '$video', 'hls': false},
          ],
        };
      case 'episodes':
        return {
          'episodes': [
            for (var i = 1; i <= episodes('${input['show_id']}'); i++) i,
          ],
        };
      case 'skip':
        return {
          'intro': [40, 130],
          'outro': [1290, 1380],
        };
      default:
        // Stopping, presence, downloads and the rest: nothing to do here.
        return {};
    }
  }
}
