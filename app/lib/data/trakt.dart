import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import 'auth.dart';
import 'models.dart';
import 'oauth_config.dart';

/// Otakase's own Trakt app. It has no secret, so its id is public by design:
/// the id alone cannot be turned into anyone's session.
const traktClientId = 'zbSBzFuTMKrNfuZyM5LawZUh-9BiKXsTyQWbbRTT9SA';
const traktRedirectUri = 'https://thexykril.github.io/otakase/';
const traktTokenKey = 'trakt_token';

class TraktFailure implements Exception {
  const TraktFailure(this.message, {this.status = 0});
  final String message;
  final int status;
  @override
  String toString() => message;
}

/// A film or a series from Trakt.
class MediaTitle {
  const MediaTitle({
    required this.kind,
    required this.trakt,
    required this.title,
    this.imdb = '',
    this.tmdb = 0,
    this.slug = '',
    this.year = 0,
    this.original = '',
    this.overview = '',
    this.tagline = '',
    this.poster = '',
    this.fanart = '',
    this.runtime = 0,
    this.genres = const [],
    this.rating = 0,
    this.certification = '',
    this.status = '',
    this.network = '',
    this.airedEpisodes = 0,
    this.language = '',
    this.votes = 0,
  });

  /// movie or show.
  final String kind;
  final int trakt, tmdb, year, runtime, airedEpisodes, votes;
  final String title,
      imdb,
      slug,
      original,
      overview,
      tagline,
      poster,
      fanart,
      certification,
      status,
      network,
      language;
  final List<String> genres;
  final double rating;
  bool get isShow => kind == 'show';

  /// Japanese animation, which the anime half of a profile already lists.
  bool get isAnime =>
      genres.contains('anime') ||
      (language == 'ja' && genres.contains('animation'));
  String get key => '$kind:$trakt';
  String get label => year > 0 ? '$title ($year)' : title;

  static String _image(Json? images, String name) {
    final list = images?[name];
    if (list is List && list.isNotEmpty && list.first is String) {
      final path = list.first as String;
      return path.startsWith('http') ? path : 'https://$path';
    }
    return '';
  }

  factory MediaTitle.fromTrakt(Json json, String kind) {
    final ids = (json['ids'] as Map? ?? {}).cast<String, dynamic>();
    final images = (json['images'] as Map?)?.cast<String, dynamic>();
    return MediaTitle(
      kind: kind,
      trakt: number(ids['trakt']),
      imdb: ids['imdb'] as String? ?? '',
      tmdb: number(ids['tmdb']),
      slug: ids['slug'] as String? ?? '',
      title: json['title'] as String? ?? '',
      year: number(json['year']),
      original: json['original_title'] as String? ?? '',
      overview: json['overview'] as String? ?? '',
      tagline: json['tagline'] as String? ?? '',
      poster: _image(images, 'poster'),
      fanart: _image(images, 'fanart'),
      runtime: number(json['runtime']),
      genres: (json['genres'] as List? ?? []).whereType<String>().toList(),
      rating: (json['rating'] as num?)?.toDouble() ?? 0,
      certification: json['certification'] as String? ?? '',
      status: json['status'] as String? ?? '',
      network: json['network'] as String? ?? '',
      airedEpisodes: number(json['aired_episodes']),
      language: json['language'] as String? ?? '',
      votes: number(json['votes']),
    );
  }

  factory MediaTitle.fromJson(Json json) => MediaTitle(
    kind: json['kind'] as String? ?? 'movie',
    trakt: number(json['trakt']),
    imdb: json['imdb'] as String? ?? '',
    tmdb: number(json['tmdb']),
    slug: json['slug'] as String? ?? '',
    title: json['title'] as String? ?? '',
    year: number(json['year']),
    original: json['original'] as String? ?? '',
    overview: json['overview'] as String? ?? '',
    tagline: json['tagline'] as String? ?? '',
    poster: json['poster'] as String? ?? '',
    fanart: json['fanart'] as String? ?? '',
    runtime: number(json['runtime']),
    genres: (json['genres'] as List? ?? []).whereType<String>().toList(),
    rating: (json['rating'] as num?)?.toDouble() ?? 0,
    certification: json['certification'] as String? ?? '',
    status: json['status'] as String? ?? '',
    network: json['network'] as String? ?? '',
    airedEpisodes: number(json['aired_episodes']),
    language: json['language'] as String? ?? '',
    votes: number(json['votes']),
  );

  Json toJson() => {
    'kind': kind,
    'trakt': trakt,
    'imdb': imdb,
    'tmdb': tmdb,
    'slug': slug,
    'title': title,
    'year': year,
    if (original.isNotEmpty) 'original': original,
    if (overview.isNotEmpty) 'overview': overview,
    if (tagline.isNotEmpty) 'tagline': tagline,
    'poster': poster,
    'fanart': fanart,
    'runtime': runtime,
    'genres': genres,
    'rating': rating,
    'certification': certification,
    'status': status,
    'network': network,
    'aired_episodes': airedEpisodes,
    'language': language,
    if (votes > 0) 'votes': votes,
  };

  /// The few fields an Otakase TV needs to find and play this title, small
  /// enough for the phone remote.
  Json get remote => {
    'kind': kind,
    'trakt': trakt,
    if (imdb.isNotEmpty) 'imdb': imdb,
    if (tmdb > 0) 'tmdb': tmdb,
    if (slug.isNotEmpty) 'slug': slug,
    'title': String.fromCharCodes(title.runes.take(200)),
    if (year > 0) 'year': year,
    if (poster.length <= 500) 'poster': poster,
    if (runtime > 0) 'runtime': runtime,
    if (language.isNotEmpty) 'language': language,
  };

  /// The ids Trakt matches a title by.
  Json get ids => {
    'trakt': trakt,
    if (imdb.isNotEmpty) 'imdb': imdb,
    if (tmdb > 0) 'tmdb': tmdb,
    if (slug.isNotEmpty) 'slug': slug,
  };
}

class TitleEpisode {
  const TitleEpisode({
    required this.season,
    required this.number,
    this.title = '',
    this.overview = '',
    this.aired,
    this.runtime = 0,
    this.thumbnail = '',
  });
  final int season, number, runtime;
  final String title, overview;

  /// A still from the episode, '' when Trakt has none.
  final String thumbnail;
  final DateTime? aired;
  String get code => 'S${season}E$number';
  String get label => title.isEmpty ? code : '$code · $title';
}

class TraktDeviceCode {
  TraktDeviceCode.fromJson(Json json)
    : deviceCode = json['device_code'] as String? ?? '',
      userCode = json['user_code'] as String? ?? '',
      verificationUrl =
          json['verification_url'] as String? ?? 'https://trakt.tv/activate',
      expiresIn = number(json['expires_in']),
      interval = number(json['interval']);
  final String deviceCode, userCode, verificationUrl;
  final int expiresIn, interval;

  /// The activation page with the code already in it, so nobody has to type
  /// it. Trakt keeps the code through its own sign-in page.
  String get activationUrl {
    final base = verificationUrl.endsWith('/')
        ? verificationUrl.substring(0, verificationUrl.length - 1)
        : verificationUrl;
    return userCode.isEmpty ? verificationUrl : '$base/$userCode';
  }
}

/// What Trakt holds for the signed-in viewer.
class TraktLibrary {
  TraktLibrary({
    this.watchlist = const [],
    this.watchedMovies = const {},
    this.watchedEpisodes = const {},
    this.playback = const [],
    this.watchedAt = const {},
    this.lastEpisodes = const {},
  });
  final List<MediaTitle> watchlist;

  /// The episode of each show watched last, and when, by show key.
  final Map<String, ({int season, int episode, int at})> lastEpisodes;

  /// When each title was last watched, in milliseconds since the epoch: by
  /// key for a film, by key and S/E code for an episode ([watchedKey]).
  final Map<String, int> watchedAt;

  /// Watched films by key.
  final Map<String, MediaTitle> watchedMovies;

  /// Watched episodes by show key, as S/E codes.
  final Map<String, ({MediaTitle title, Set<String> codes})> watchedEpisodes;
  final List<TraktPaused> playback;
}

class TraktPaused {
  const TraktPaused(
    this.title,
    this.progress, {
    this.season = 0,
    this.episode = 0,
    this.pausedAt = 0,
    this.id = 0,
  });
  final MediaTitle title;
  final double progress;
  final int season, episode;

  /// Trakt's id for this paused spot, to remove it once it is stale.
  final int id;

  /// When it was paused, in milliseconds since the epoch; 0 when unknown.
  final int pausedAt;
}

/// The [TraktLibrary.watchedAt] key for a film, or for one episode of a show.
String watchedKey(MediaTitle title, {int season = 0, int episode = 0}) =>
    title.isShow ? '${title.key}:S${season}E$episode' : title.key;

int _pausedAt(Object? value) =>
    value is String ? DateTime.tryParse(value)?.millisecondsSinceEpoch ?? 0 : 0;

/// Trakt for one profile: the public catalog, the device sign-in, and the
/// viewer's watchlist, history and progress. The token is kept with the
/// profile's other sign-ins.
class Trakt extends ChangeNotifier {
  Trakt({required this.client, required this.storage, DateTime Function()? now})
    : now = now ?? DateTime.now;
  final http.Client client;
  final TokenStore storage;
  final DateTime Function() now;
  static const api = 'https://api.trakt.tv';
  Json? _token;
  String username = '';
  bool _loaded = false;
  bool disposed = false;
  String? lastError;
  bool get signedIn => _token != null;

  /// Reads the saved login again, after the account vault brought one.
  Future<void> reload() {
    _loaded = false;
    return load();
  }

  Future<void> load() async {
    if (_loaded) return;
    _loaded = true;
    try {
      final raw = await storage.read(traktTokenKey);
      if (raw != null && raw.isNotEmpty) {
        _token = (jsonDecode(raw) as Map).cast<String, dynamic>();
        username = _token?['username'] as String? ?? '';
      }
    } catch (_) {
      _token = null;
    }
    _notify();
  }

  void _notify() {
    if (!disposed) notifyListeners();
  }

  @override
  void dispose() {
    disposed = true;
    unawaited(cancelBrowserSignIn());
    super.dispose();
  }

  Map<String, String> _headers([String? bearer]) => {
    'Content-Type': 'application/json',
    'trakt-api-version': '2',
    'trakt-api-key': traktClientId,
    if (bearer != null) 'Authorization': 'Bearer $bearer',
  };

  Future<http.Response> _send(
    String method,
    String path, {
    Object? body,
    bool auth = false,
  }) async {
    Future<http.Response> attempt(String? bearer) async {
      final request = http.Request(method, Uri.parse('$api$path'))
        ..headers.addAll(_headers(bearer));
      if (body != null) request.body = jsonEncode(body);
      return http.Response.fromStream(
        await client.send(request).timeout(const Duration(seconds: 20)),
      );
    }

    var bearer = auth ? await _bearer() : null;
    var response = await attempt(bearer);
    if (response.statusCode == 401 && auth) {
      // A refresh elsewhere may have replaced this access token, or it ran
      // out early. Try once more with a fresh one before giving up.
      final current = _token?['access_token'] as String?;
      bearer = current != null && current != bearer
          ? current
          : await _bearer(force: true);
      response = await attempt(bearer);
      if (response.statusCode == 401) {
        // The session was revoked on trakt.tv.
        if (_token?['access_token'] == bearer) await signOut();
        throw const TraktFailure(
          'Trakt sign-in expired. Sign in again.',
          status: 401,
        );
      }
    }
    if (response.statusCode >= 400) {
      throw TraktFailure(
        'Trakt answered ${response.statusCode}.',
        status: response.statusCode,
      );
    }
    return response;
  }

  Future<dynamic> _get(String path, {bool auth = false}) async {
    final response = await _send('GET', path, auth: auth);
    return response.bodyBytes.isEmpty
        ? null
        : jsonDecode(utf8.decode(response.bodyBytes, allowMalformed: true));
  }

  /// Every page of a paged list. Trakt's watched lists hand out one page
  /// of 100 at a time since July 2026; a call without paging gets only the
  /// first. Stops at the page count Trakt gives, at an empty page, or at a
  /// page that only repeats rows already seen (Trakt answers a page past
  /// the end with the last one again).
  Future<List<dynamic>> _getAll(
    String path, {
    bool auth = false,
    int limit = 100,
    int maxPages = 100,
  }) async {
    final rows = <dynamic>[];
    final seen = <String>{};
    final join = path.contains('?') ? '&' : '?';
    for (var page = 1; page <= maxPages; page++) {
      final response = await _send(
        'GET',
        '$path${join}page=$page&limit=$limit',
        auth: auth,
      );
      final data = response.bodyBytes.isEmpty
          ? null
          : jsonDecode(utf8.decode(response.bodyBytes, allowMalformed: true));
      if (data is! List || data.isEmpty) break;
      var fresh = 0;
      for (final row in data) {
        if (seen.add(jsonEncode(row))) {
          rows.add(row);
          fresh++;
        }
      }
      if (fresh == 0) break;
      final count = int.tryParse(
        response.headers['x-pagination-page-count'] ?? '',
      );
      // No paging headers: the whole list came in one answer.
      if (count == null || page >= count) break;
    }
    return rows;
  }

  Future<Json>? _refreshing;

  Future<String> _bearer({bool force = false}) async {
    await load();
    final token = _token;
    if (token == null) throw const TraktFailure('Not signed in to Trakt.');
    final created = number(token['created_at']);
    final expires = number(token['expires_in']);
    if (force ||
        (expires > 0 &&
            now().millisecondsSinceEpoch ~/ 1000 > created + expires - 60)) {
      // Trakt hands out a new refresh token with every refresh and the old
      // one stops working, so calls that start together share one refresh.
      final fresh = await (_refreshing ??= _refresh(token)
          .whenComplete(() => _refreshing = null));
      return fresh['access_token'] as String;
    }
    return token['access_token'] as String;
  }

  /// Swaps [token] for a fresh one. Only a refused refresh signs out; when
  /// Trakt or the network is down the saved sign-in stays for next time.
  Future<Json> _refresh(Json token) async {
    final refresh = token['refresh_token'];
    if (refresh is! String || refresh.isEmpty) {
      if (identical(_token, token)) await signOut();
      throw const TraktFailure('Trakt sign-in expired. Sign in again.');
    }
    http.Response response;
    try {
      response = await client
          .post(
            Uri.parse('$api/oauth/token'),
            headers: _headers(),
            body: jsonEncode({
              'refresh_token': refresh,
              'client_id': traktClientId,
              'redirect_uri': _redirect(token),
              'grant_type': 'refresh_token',
            }),
          )
          .timeout(const Duration(seconds: 20));
    } catch (_) {
      throw const TraktFailure('Trakt could not be reached. Try again.');
    }
    if (response.statusCode == 400 || response.statusCode == 401) {
      // A newer sign-in may have landed while this one was out.
      if (identical(_token, token)) await signOut();
      throw const TraktFailure('Trakt sign-in expired. Sign in again.');
    }
    if (response.statusCode != 200) {
      throw TraktFailure(
        'Trakt answered ${response.statusCode}.',
        status: response.statusCode,
      );
    }
    final fresh = (jsonDecode(response.body) as Map).cast<String, dynamic>();
    if (token['redirect_uri'] case final String redirect) {
      fresh['redirect_uri'] = redirect;
    }
    await _save(fresh);
    return fresh;
  }

  Future<void> _save(Json token) async {
    token['username'] = username;
    _token = token;
    await storage.write(traktTokenKey, jsonEncode(token));
    _notify();
  }

  /// The redirect a token was issued with; refreshes must send the same one.
  static String _redirect(Json token) =>
      token['redirect_uri'] as String? ?? traktRedirectUri;

  HttpServer? _server;
  Completer<bool>? _browser;
  String? _browserState, _verifier;
  Timer? _browserExpiry;

  /// Starts the desktop sign-in: Trakt's page opens in the browser on this
  /// computer, and the sign-in page hands the approval back to the app on
  /// 127.0.0.1 ([AuthService.desktopLoginPort]). PKCE, so no client secret.
  /// [browserSignIn] completes when it finishes.
  Future<Uri> beginBrowserSignIn({
    int port = AuthService.desktopLoginPort,
  }) async {
    await cancelBrowserSignIn();
    final server = await () async {
      try {
        return await HttpServer.bind(InternetAddress.loopbackIPv4, port);
      } on SocketException {
        throw const TraktFailure(
          'Another sign-in is still open. Close it and try again.',
        );
      }
    }();
    _server = server;
    final done = _browser = Completer<bool>();
    String random() =>
        base64UrlEncode(List.generate(48, (_) => Random.secure().nextInt(256)))
            .replaceAll('=', '');
    final verifier = _verifier = random();
    final state = _browserState = base64UrlEncode(
      utf8.encode(
        jsonEncode({'nonce': random(), 'service': 'Trakt', 'desktop': true}),
      ),
    ).replaceAll('=', '');
    server.listen(_receive, onError: (_) {});
    _browserExpiry = Timer(const Duration(minutes: 10), () {
      if (_browser == done) unawaited(cancelBrowserSignIn());
    });
    return Uri.https('trakt.tv', '/oauth/authorize', {
      'response_type': 'code',
      'client_id': traktClientId,
      'redirect_uri': loginRedirect,
      'state': state,
      'code_challenge': base64UrlEncode(
        sha256.convert(ascii.encode(verifier)).bytes,
      ).replaceAll('=', ''),
      'code_challenge_method': 'S256',
    });
  }

  /// True once the browser sign-in saved a token; false when it was
  /// cancelled, declined or expired.
  Future<bool> get browserSignIn => _browser?.future ?? Future.value(false);

  Future<void> cancelBrowserSignIn() async {
    _browserExpiry?.cancel();
    _browserExpiry = null;
    final done = _browser;
    _browser = null;
    _browserState = null;
    _verifier = null;
    final server = _server;
    _server = null;
    await server?.close(force: true);
    if (done != null && !done.isCompleted) done.complete(false);
  }

  Future<void> _receive(HttpRequest request) async {
    request.response.headers.set('Cache-Control', 'no-store');
    request.response.headers.set('Referrer-Policy', 'no-referrer');
    request.response.headers.contentType = ContentType.html;
    final q = request.uri.queryParameters;
    final done = _browser;
    final verifier = _verifier;
    var success = false;
    try {
      if (request.method != 'GET' || request.uri.path != '/login') {
        request.response.statusCode = HttpStatus.notFound;
        return;
      }
      if (done == null ||
          verifier == null ||
          q['state'] != _browserState ||
          (q['code'] ?? '').isEmpty) {
        throw const TraktFailure('Login expired.');
      }
      // One code per sign-in: a second request with the same state fails.
      _browserState = null;
      final response = await client
          .post(
            Uri.parse('$api/oauth/token'),
            headers: _headers(),
            body: jsonEncode({
              'code': q['code'],
              'client_id': traktClientId,
              'redirect_uri': loginRedirect,
              'grant_type': 'authorization_code',
              'code_verifier': verifier,
            }),
          )
          .timeout(const Duration(seconds: 20));
      if (response.statusCode != 200) {
        throw TraktFailure('Trakt answered ${response.statusCode}.');
      }
      final token = (jsonDecode(response.body) as Map).cast<String, dynamic>()
        ..['redirect_uri'] = loginRedirect;
      if (_browser != done) throw const TraktFailure('Login cancelled.');
      await _signedIn(token);
      success = true;
      request.response.write(
        '<!doctype html><meta name="viewport" content="width=device-width"><title>Otakase</title><p>Signed in to Trakt. Return to Otakase.</p>',
      );
    } catch (_) {
      request.response.statusCode = HttpStatus.badRequest;
      request.response.write(
        '<!doctype html><title>Otakase</title><p>Login expired or unsuccessful. Start again in Otakase.</p>',
      );
    } finally {
      await request.response.close();
      if (success && done != null) {
        if (!done.isCompleted) done.complete(true);
        if (_browser == done) await cancelBrowserSignIn();
      }
    }
  }

  /// Keeps a fresh token and looks up whose it is.
  Future<void> _signedIn(Json token) async {
    await load();
    _token = token;
    try {
      final settings = await _get('/users/settings', auth: true) as Map;
      username = ((settings['user'] as Map?)?['username'] as String?) ?? '';
    } catch (_) {
      username = '';
    }
    await _save(token);
  }

  Future<TraktDeviceCode> startSignIn() async {
    final response = await _send(
      'POST',
      '/oauth/device/code',
      body: {'client_id': traktClientId},
    );
    return TraktDeviceCode.fromJson(
      (jsonDecode(response.body) as Map).cast<String, dynamic>(),
    );
  }

  /// Polls until the viewer enters the code at trakt.tv/activate. Returns
  /// false when it expired, was denied or [cancelled] turned true.
  Future<bool> finishSignIn(
    TraktDeviceCode code, {
    required bool Function() cancelled,
    Duration? interval,
  }) async {
    var wait = interval ?? Duration(seconds: code.interval.clamp(5, 30));
    final deadline = now().add(
      Duration(seconds: code.expiresIn.clamp(60, 1800)),
    );
    while (now().isBefore(deadline) && !cancelled()) {
      await Future<void>.delayed(wait);
      if (cancelled()) return false;
      http.Response response;
      try {
        response = await client
            .post(
              Uri.parse('$api/oauth/device/token'),
              headers: _headers(),
              body: jsonEncode({
                'code': code.deviceCode,
                'client_id': traktClientId,
              }),
            )
            .timeout(const Duration(seconds: 20));
      } catch (_) {
        continue;
      }
      switch (response.statusCode) {
        case 200:
          await _signedIn(
            (jsonDecode(response.body) as Map).cast<String, dynamic>(),
          );
          return true;
        case 400:
          continue;
        case 429:
          wait += const Duration(seconds: 1);
          continue;
        default:
          return false;
      }
    }
    return false;
  }

  Future<void> signOut() async {
    _token = null;
    username = '';
    await storage.write(traktTokenKey, null);
    _notify();
  }

  // ------------------------------------------------------------- catalog

  static String _plural(String kind) => kind == 'show' ? 'shows' : 'movies';

  List<MediaTitle> _titles(dynamic rows, String kind) => [
    for (final row in (rows as List? ?? []).whereType<Map>())
      if ((row[kind] ?? row) case final Map item)
        MediaTitle.fromTrakt(item.cast<String, dynamic>(), kind),
  ].where((t) => t.trakt > 0 && t.title.isNotEmpty).toList();

  static String _filters(Map<String, String> filters) => [
    for (final f in filters.entries)
      '&${f.key}=${Uri.encodeQueryComponent(f.value)}',
  ].join();

  /// trending, popular, anticipated or watched (this week).
  Future<List<MediaTitle>> list(
    String kind,
    String list, {
    int limit = 20,
    Map<String, String> filters = const {},
  }) async {
    final path = list == 'watched' ? 'watched/weekly' : list;
    return _titles(
      await _get(
        '/${_plural(kind)}/$path?extended=full,images&limit=$limit${_filters(filters)}',
      ),
      kind,
    );
  }

  Future<List<MediaTitle>> search(
    String query, {
    String kinds = 'movie,show',
    Map<String, String> filters = const {},
  }) async {
    final rows =
        await _get(
          '/search/$kinds?query=${Uri.encodeQueryComponent(query)}&extended=full,images&limit=30${_filters(filters)}',
        ) as List? ??
        [];
    return [
      for (final row in rows.whereType<Map>())
        if (row[row['type']] case final Map item)
          MediaTitle.fromTrakt(
            item.cast<String, dynamic>(),
            row['type'] as String,
          ),
    ].where((t) => t.trakt > 0).toList();
  }

  /// The film or series with this IMDb id, as a Discord share names it.
  Future<MediaTitle?> byImdb(String imdb, {String? kind}) async {
    if (!RegExp(r'^tt\d{5,10}$').hasMatch(imdb)) return null;
    final rows =
        await _get(
          '/search/imdb/$imdb?type=${kind ?? 'movie,show'}&extended=full,images',
        ) as List? ??
        [];
    for (final row in rows.whereType<Map>()) {
      if (row[row['type']] case final Map item) {
        final t = MediaTitle.fromTrakt(
          item.cast<String, dynamic>(),
          row['type'] as String,
        );
        if (t.trakt > 0) return t;
      }
    }
    return null;
  }

  Future<MediaTitle> details(MediaTitle title) async {
    final json = await _get(
      '/${_plural(title.kind)}/${title.trakt}?extended=full,images',
    );
    return MediaTitle.fromTrakt(
      (json as Map).cast<String, dynamic>(),
      title.kind,
    );
  }

  Future<List<MediaTitle>> related(MediaTitle title) async => _titles(
    await _get(
      '/${_plural(title.kind)}/${title.trakt}/related?extended=full,images&limit=12',
    ),
    title.kind,
  );

  /// The title in another language, such as lt, or '' when Trakt has none.
  Future<String> translation(MediaTitle title, String language) async {
    try {
      final rows =
          await _get(
            '/${_plural(title.kind)}/${title.trakt}/translations/$language',
          ) as List? ??
          [];
      for (final row in rows.whereType<Map>()) {
        final t = (row['title'] as String? ?? '').trim();
        if (t.isNotEmpty) return t;
      }
    } catch (_) {
      /* A missing translation only means fewer titles to search by. */
    }
    return '';
  }

  /// A series' aired episodes by season, specials left out.
  Future<List<TitleEpisode>> episodes(MediaTitle show) async {
    final seasons =
        await _get('/shows/${show.trakt}/seasons?extended=full,images,episodes')
            as List? ??
        [];
    final today = now();
    final out = <TitleEpisode>[];
    for (final season in seasons.whereType<Map>()) {
      final n = number(season['number']);
      if (n == 0) continue;
      for (final e in (season['episodes'] as List? ?? []).whereType<Map>()) {
        final aired = DateTime.tryParse(e['first_aired'] as String? ?? '');
        if (aired == null || aired.isAfter(today)) continue;
        out.add(
          TitleEpisode(
            season: n,
            number: number(e['number']),
            title: e['title'] as String? ?? '',
            overview: e['overview'] as String? ?? '',
            aired: aired,
            runtime: number(e['runtime']),
            thumbnail: MediaTitle._image(
              (e['images'] as Map?)?.cast<String, dynamic>(),
              'screenshot',
            ),
          ),
        );
      }
    }
    return out;
  }

  // ---------------------------------------------------------------- sync

  /// Episodes of the viewer's shows (watched or on the watchlist) airing in
  /// the [days] days from [start], from Trakt's calendar.
  Future<List<({MediaTitle show, TitleEpisode episode, DateTime at})>> calendar(
    DateTime start,
    int days,
  ) async {
    final day =
        '${start.year}-${start.month.toString().padLeft(2, '0')}-${start.day.toString().padLeft(2, '0')}';
    final list =
        await _get(
          '/calendars/my/shows/$day/${days.clamp(1, 31)}?extended=full,images',
          auth: true,
        ) as List? ??
        [];
    return [
      for (final item in list.whereType<Map>())
        if (DateTime.tryParse(item['first_aired'] as String? ?? '')
            case final at?)
          if (item['show'] is Map && item['episode'] is Map)
            (
              show: MediaTitle.fromTrakt(
                (item['show'] as Map).cast<String, dynamic>(),
                'show',
              ),
              episode: TitleEpisode(
                season: number((item['episode'] as Map)['season']),
                number: number((item['episode'] as Map)['number']),
                title: (item['episode'] as Map)['title'] as String? ?? '',
                runtime: number((item['episode'] as Map)['runtime']),
                aired: at,
              ),
              at: at.toLocal(),
            ),
    ];
  }

  Future<TraktLibrary> library() async {
    final results = await Future.wait([
      _getAll('/sync/watchlist/movies?extended=full,images', auth: true),
      _getAll('/sync/watchlist/shows?extended=full,images', auth: true),
      _getAll('/sync/watched/movies', auth: true),
      // Since July 2026 the seasons and episodes only come with
      // extended=progress; without it every show reads as unwatched.
      _getAll('/sync/watched/shows?extended=progress', auth: true),
      _get('/sync/playback?extended=full,images', auth: true),
    ]);
    final watchlist = [
      ..._titles(results[0], 'movie'),
      ..._titles(results[1], 'show'),
    ];
    final movies = <String, MediaTitle>{
      for (final t in _titles(results[2], 'movie')) t.key: t,
    };
    final watchedAt = <String, int>{};
    for (final row in (results[2] as List? ?? []).whereType<Map>()) {
      if (row['movie'] case final Map movie) {
        final t = MediaTitle.fromTrakt(movie.cast<String, dynamic>(), 'movie');
        final at = _pausedAt(row['last_watched_at']);
        if (at > 0) watchedAt[t.key] = at;
      }
    }
    final episodes = <String, ({MediaTitle title, Set<String> codes})>{};
    final lastEpisodes = <String, ({int season, int episode, int at})>{};
    for (final row in (results[3] as List? ?? []).whereType<Map>()) {
      final show = row['show'];
      if (show is! Map) continue;
      final title = MediaTitle.fromTrakt(show.cast<String, dynamic>(), 'show');
      final seen = episodes
          .putIfAbsent(title.key, () => (title: title, codes: <String>{}))
          .codes;
      for (final s in (row['seasons'] as List? ?? []).whereType<Map>()) {
        for (final e in (s['episodes'] as List? ?? []).whereType<Map>()) {
          final season = number(s['number']), episode = number(e['number']);
          seen.add('S${season}E$episode');
          final at = _pausedAt(e['last_watched_at']);
          if (at > 0) {
            watchedAt[watchedKey(title, season: season, episode: episode)] = at;
            final last = lastEpisodes[title.key];
            if (last == null || at > last.at) {
              lastEpisodes[title.key] = (
                season: season,
                episode: episode,
                at: at,
              );
            }
          }
        }
      }
    }
    return TraktLibrary(
      watchlist: watchlist,
      watchedMovies: movies,
      watchedEpisodes: episodes,
      playback: _paused(results[4]),
      watchedAt: watchedAt,
      lastEpisodes: lastEpisodes,
    );
  }

  /// The next episode to watch in [show] by the viewer's Trakt history,
  /// specials left out; null once every aired episode is watched.
  Future<({int season, int episode})?> nextEpisode(MediaTitle show) async {
    final data = await _get(
      '/shows/${show.trakt}/progress/watched?specials=false&count_specials=false',
      auth: true,
    );
    if (data is! Map || data['next_episode'] is! Map) return null;
    final next = data['next_episode'] as Map;
    return (season: number(next['season']), episode: number(next['number']));
  }

  /// A fingerprint of when the viewer's films, episodes and lists last
  /// changed on Trakt; while it stays the same the library has not changed.
  /// Empty when Trakt does not say.
  Future<String> lastActivity() async {
    final data = await _get('/sync/last_activities', auth: true);
    if (data is! Map) return '';
    final parts = {
      for (final k in ['movies', 'episodes', 'shows', 'seasons'])
        if (data[k] is Map) k: data[k],
    };
    return parts.isEmpty ? '' : jsonEncode(parts);
  }

  /// Drops a paused spot from Trakt; one already gone counts as dropped.
  Future<void> removePlayback(int id) async {
    try {
      await _send('DELETE', '/sync/playback/$id', auth: true);
    } on TraktFailure catch (e) {
      if (e.status != 404) rethrow;
    }
  }

  /// Paused films and episodes only, for a quick check before playing.
  Future<List<TraktPaused>> playback() async =>
      _paused(await _get('/sync/playback?extended=full,images', auth: true));

  List<TraktPaused> _paused(dynamic rows) {
    final playback = <TraktPaused>[];
    for (final row in (rows as List? ?? []).whereType<Map>()) {
      final progress = (row['progress'] as num?)?.toDouble() ?? 0;
      final pausedAt = _pausedAt(row['paused_at']);
      final id = number(row['id']);
      if (row['type'] == 'movie' && row['movie'] is Map) {
        playback.add(
          TraktPaused(
            MediaTitle.fromTrakt(
              (row['movie'] as Map).cast<String, dynamic>(),
              'movie',
            ),
            progress,
            pausedAt: pausedAt,
            id: id,
          ),
        );
      } else if (row['type'] == 'episode' &&
          row['show'] is Map &&
          row['episode'] is Map) {
        final e = row['episode'] as Map;
        playback.add(
          TraktPaused(
            MediaTitle.fromTrakt(
              (row['show'] as Map).cast<String, dynamic>(),
              'show',
            ),
            progress,
            season: number(e['season']),
            episode: number(e['number']),
            pausedAt: pausedAt,
            id: id,
          ),
        );
      }
    }
    return playback;
  }

  Json _item(
    MediaTitle title, {
    int season = 0,
    int episode = 0,
    int watchedAt = 0,
  }) {
    final at = watchedAt > 0
        ? {
            'watched_at': DateTime.fromMillisecondsSinceEpoch(
              watchedAt,
              isUtc: true,
            ).toIso8601String(),
          }
        : const <String, dynamic>{};
    return title.isShow
        ? {
            'shows': [
              {
                'ids': title.ids,
                if (season > 0)
                  'seasons': [
                    {
                      'number': season,
                      if (episode > 0)
                        'episodes': [
                          {'number': episode, ...at},
                        ],
                    },
                  ],
              },
            ],
          }
        : {
            'movies': [
              {'ids': title.ids, ...at},
            ],
          };
  }

  /// Gives the film or show a score from 1 to 10 on Trakt.
  Future<void> rate(MediaTitle title, int rating) => _send(
    'POST',
    '/sync/ratings',
    body: {
      title.isShow ? 'shows' : 'movies': [
        {'ids': title.ids, 'rating': rating.clamp(1, 10)},
      ],
    },
    auth: true,
  );

  Future<void> setWatchlist(MediaTitle title, bool on) => _send(
    'POST',
    on ? '/sync/watchlist' : '/sync/watchlist/remove',
    body: _item(title),
    auth: true,
  );

  /// Adds a watch to the history, at [watchedAt] (ms since the epoch) when
  /// it happened earlier, or takes the title's watches off it.
  Future<void> setWatched(
    MediaTitle title,
    bool watched, {
    int season = 0,
    int episode = 0,
    int watchedAt = 0,
  }) => _send(
    'POST',
    watched ? '/sync/history' : '/sync/history/remove',
    body: _item(
      title,
      season: season,
      episode: episode,
      watchedAt: watched ? watchedAt : 0,
    ),
    auth: true,
  );

  /// Tells Trakt what is playing: start, pause or stop. Stop at 80% or more
  /// marks it watched there.
  Future<void> scrobble(
    String action,
    MediaTitle title,
    double progress, {
    int season = 0,
    int episode = 0,
  }) => _send(
    'POST',
    '/scrobble/$action',
    body: {
      if (title.isShow) ...{
        'show': {'ids': title.ids},
        'episode': {'season': season, 'number': episode},
      } else
        'movie': {'ids': title.ids},
      'progress': double.parse(progress.clamp(0, 100).toStringAsFixed(2)),
    },
    auth: true,
  );
}

/// Search genres with their Trakt genre. Anime genres that Trakt has share
/// its name here; the rest only exist for films and series.
const _traktGenres = {
  'Action': 'action',
  'Adventure': 'adventure',
  'Animation': 'animation',
  'Anime': 'anime',
  'Comedy': 'comedy',
  'Crime': 'crime',
  'Documentary': 'documentary',
  'Drama': 'drama',
  'Family': 'family',
  'Fantasy': 'fantasy',
  'History': 'history',
  'Holiday': 'holiday',
  'Horror': 'horror',
  'Music': 'music',
  'Musical': 'musical',
  'Mystery': 'mystery',
  'Romance': 'romance',
  'Sci-Fi': 'science-fiction',
  'Sports': 'sports',
  'Superhero': 'superhero',
  'Suspense': 'suspense',
  'Thriller': 'thriller',
  'War': 'war',
  'Western': 'western',
};

/// The genres movie and show search can filter by.
final traktGenreNames = _traktGenres.keys.toList();

/// Original languages Search can filter films and series by.
const titleLanguages = {
  'en': 'English',
  'ja': 'Japanese',
  'ko': 'Korean',
  'zh': 'Chinese',
  'lt': 'Lithuanian',
  'fr': 'French',
  'es': 'Spanish',
  'de': 'German',
  'it': 'Italian',
  'ru': 'Russian',
  'hi': 'Hindi',
};

/// Age ratings as "suitable up to": each holds the ones before it.
const titleAges = {'KIDS': 'Kids', 'FAMILY': 'Family', 'TEENS': 'Teens'};
const _movieAges = {'KIDS': 'g', 'FAMILY': 'g,pg', 'TEENS': 'g,pg,pg-13'};
const _showAges = {
  'KIDS': 'tv-y,tv-y7,tv-g',
  'FAMILY': 'tv-y,tv-y7,tv-g,tv-pg',
  'TEENS': 'tv-y,tv-y7,tv-g,tv-pg,tv-14',
};

/// Length: a film's running time, or a series' episode length.
const titleLengths = {'SHORT': 'Short', 'MEDIUM': 'Medium', 'LONG': 'Long'};
const _movieLengths = {'SHORT': '1-89', 'MEDIUM': '90-120', 'LONG': '121-999'};
const _showLengths = {'SHORT': '1-29', 'MEDIUM': '30-60', 'LONG': '61-999'};

/// The Trakt lookups that match the anime search filters: one per kind
/// (movie or show) with its query filters. Empty when no film or series can
/// match, like an OVA format or a genre Trakt does not have. A season only
/// narrows to its year; Trakt status filters exist for series only. A
/// [decade] is its first year (2010 for the 2010s), [minScore] a 0-100
/// rating floor; [language], [age] and [length] are films and series only.
List<(String, Map<String, String>)> traktFilterRequests({
  String? format,
  String? season,
  int? year,
  String? genre,
  String? status,
  int? decade,
  int? minScore,
  String? language,
  String? age,
  String? length,
  String? sort,
}) {
  final kinds = switch (format) {
    // The box office is a film chart.
    null when sort == 'BOXOFFICE' => ['movie'],
    null => ['movie', 'show'],
    'MOVIE' => ['movie'],
    'TV' || 'TV_SHORT' when sort != 'BOXOFFICE' => ['show'],
    _ => <String>[],
  };
  final common = <String, String>{
    if (year != null)
      'years': '$year'
    else if (decade != null)
      'years': '$decade-${decade + 9}',
    if (minScore != null) 'ratings': '$minScore-100',
    'languages': ?language,
  };
  if (genre != null) {
    final slug = _traktGenres[genre];
    if (slug == null) return const [];
    common['genres'] = slug;
  }
  final showStatus = switch (status) {
    null => null,
    'RELEASING' => 'returning series',
    'FINISHED' => 'ended',
    'NOT_YET_RELEASED' => 'planned,in production',
    'CANCELLED' => 'canceled',
    _ => '',
  };
  if (showStatus == '') return const [];
  return [
    for (final kind in kinds)
      if (kind == 'show')
        (
          'show',
          {
            ...common,
            'status': ?showStatus,
            'certifications': ?_showAges[age],
            'runtimes': ?_showLengths[length],
          },
        )
      // A film has no airing status; only a finished one can match.
      else if (status == null || status == 'FINISHED')
        (
          'movie',
          {
            ...common,
            'certifications': ?_movieAges[age],
            'runtimes': ?_movieLengths[length],
          },
        ),
  ];
}

/// Films and series for a search: Trakt search with [query] when there is
/// one, else its trending or popular lists, narrowed by the search filters
/// and put in [sort] order (null keeps relevance, or the list's own order).
Future<List<MediaTitle>> findTitles(
  Trakt trakt,
  String query, {
  String? format,
  String? season,
  int? year,
  String? genre,
  String? status,
  String? sort,
  int? decade,
  int? minScore,
  String? language,
  String? age,
  String? length,
}) async {
  final q = query.trim();
  final requests = traktFilterRequests(
    format: format,
    season: season,
    year: year,
    genre: genre,
    status: status,
    decade: decade,
    minScore: minScore,
    language: language,
    age: age,
    length: length,
    sort: sort,
  );
  if (requests.isEmpty) return const [];
  final List<List<MediaTitle>> found;
  if (q.isNotEmpty &&
      requests.length == 2 &&
      mapEquals(requests[0].$2, requests[1].$2)) {
    // One search keeps films and series in order of relevance.
    found = [await trakt.search(q, filters: requests[0].$2)];
  } else {
    found = await Future.wait([
      for (final (kind, filters) in requests)
        q.isNotEmpty
            ? trakt.search(q, kinds: kind, filters: filters)
            : trakt.list(
                kind,
                switch (sort) {
                  'TRENDING' => 'trending',
                  'ANTICIPATED' => 'anticipated',
                  'WATCHED' => 'watched',
                  'BOXOFFICE' => 'boxoffice',
                  _ => 'popular',
                },
                limit: sort == null || sort == 'TRENDING' ? 20 : 40,
                filters: filters,
              ),
    ]);
  }
  final longest = found.fold(0, (n, l) => max(n, l.length));
  final merged = [
    for (var i = 0; i < longest; i++)
      for (final list in found)
        if (i < list.length) list[i],
  ];
  // Trakt's lists already come in their own order.
  return q.isEmpty && listSorts.contains(sort)
      ? merged
      : sortTitles(merged, sort);
}

/// Sorts that are Trakt lists of their own; with a title typed they keep
/// relevance, except the ones [sortTitles] can order by.
const listSorts = {
  'POPULAR',
  'TRENDING',
  'ANTICIPATED',
  'WATCHED',
  'BOXOFFICE',
};

/// Sorts only films and series have, for the filters.
const titleOnlySorts = {
  'ANTICIPATED': 'Anticipated',
  'WATCHED': 'Most watched',
  'BOXOFFICE': 'Box office',
};

/// [titles] in a search sort order: POPULAR and TRENDING by votes, SCORE by
/// rating, NEWEST by year. Ties keep their order; null keeps them all.
List<MediaTitle> sortTitles(List<MediaTitle> titles, String? sort) {
  final int Function(MediaTitle, MediaTitle)? compare = switch (sort) {
    'POPULAR' || 'TRENDING' => (a, b) => b.votes.compareTo(a.votes),
    'SCORE' => (a, b) => b.rating.compareTo(a.rating),
    'NEWEST' => (a, b) => b.year.compareTo(a.year),
    _ => null,
  };
  if (compare == null) return titles;
  final indexed = titles.indexed.toList()
    ..sort((a, b) {
      final c = compare(a.$2, b.$2);
      return c != 0 ? c : a.$1.compareTo(b.$1);
    });
  return [for (final (_, t) in indexed) t];
}
