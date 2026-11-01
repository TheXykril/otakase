import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/foundation.dart';

import 'discord.dart';
import 'models.dart';
import 'store.dart';

// Synced watch parties, as the CLI had them: a party planned in the Discord
// with /watchparty sync:True (or one started from here with Watch with me)
// has one host whose player everyone else follows. Each app keeps a
// WebSocket to the bot (GET /v1/party/{id}/ws, bot/partysync.go); the host
// sends its player state and the bot relays it to the members.

/// How far a member may drift from the host before it seeks (seconds).
const partyDriftLimit = 2.0;

/// Lets a seek land before the position is judged again.
const partySeekCooldown = Duration(seconds: 3);

/// Added when seeking into a playing episode: the seek takes about this long.
const partySeekLead = 0.3;

/// How often the host sends its state with nothing changing.
const partyStateEvery = Duration(seconds: 5);

/// Caps how much a state's age adds to the host's position (seconds).
const partyMaxAge = 30.0;

/// A Watch with me party ends after this long without playback.
const watchWithMeIdle = Duration(seconds: 90);

/// The host's player, as the host sends it and the bot relays it. [sentAt]
/// is unix ms in the bot's clock.
@immutable
class PartyHostState {
  const PartyHostState({
    required this.anilistId,
    required this.episode,
    required this.playing,
    required this.position,
    this.sentAt = 0,
  });
  factory PartyHostState.fromJson(Json j) => PartyHostState(
    anilistId: number(j['anilist_id']),
    episode: number(j['episode']),
    playing: j['playing'] == true,
    position: (j['position'] as num?)?.toDouble() ?? 0,
    sentAt: number(j['sent_at']),
  );
  final int anilistId, episode, sentAt;
  final bool playing;
  final double position;
  Json toJson() => {
    'type': 'state',
    'anilist_id': anilistId,
    'episode': episode,
    'playing': playing,
    'position': position,
    'sent_at': sentAt,
  };
}

/// Where the host is at [serverNow] (bot clock, ms).
double partyHostPositionAt(PartyHostState s, int serverNow) {
  if (!s.playing || s.sentAt == 0) return s.position;
  final age = ((serverNow - s.sentAt) / 1000).clamp(0, partyMaxAge);
  return s.position + age;
}

/// A member's own player.
class PartyLocal {
  const PartyLocal({
    required this.anilistId,
    required this.episode,
    required this.loaded,
    required this.paused,
    required this.position,
    required this.sinceSeek,
  });
  final int anilistId, episode;
  final bool loaded, paused;
  final double position;
  final Duration sinceSeek;
}

/// What a member does to follow the host.
class PartyAction {
  bool open = false, switchEpisode = false, pause = false, play = false;
  bool seek = false;
  double seekTo = 0;
  String note = '';
}

/// Compares a member's player with the host's state.
PartyAction decidePartySync(PartyHostState h, PartyLocal? l, int serverNow) {
  final a = PartyAction();
  if (h.episode <= 0 || h.anilistId <= 0) return a;
  if (l == null || l.anilistId != h.anilistId) {
    a.open = true;
    a.note = 'Opening the host’s show';
    return a;
  }
  if (h.episode != l.episode) {
    a.switchEpisode = true;
    a.note = 'Host switched to episode ${h.episode}';
    return a;
  }
  if (!l.loaded) return a;
  final notes = <String>[];
  if (!h.playing && !l.paused) {
    a.pause = true;
    notes.add('Host paused');
  } else if (h.playing && l.paused) {
    a.play = true;
    notes.add('Host resumed');
  }
  final target = partyHostPositionAt(h, serverNow);
  if ((l.position - target).abs() > partyDriftLimit &&
      l.sinceSeek >= partySeekCooldown) {
    a.seek = true;
    a.seekTo = target + (h.playing ? partySeekLead : 0);
    notes.add('Synced to host');
  }
  a.note = notes.join(' · ');
  return a;
}

/// Whether the host sends [cur], given the last state it sent and when.
bool partyShouldSend(
  PartyHostState? last,
  DateTime lastAt,
  PartyHostState cur,
  DateTime now,
) {
  if (cur.anilistId <= 0 || cur.episode <= 0) return false;
  if (last == null ||
      cur.anilistId != last.anilistId ||
      cur.episode != last.episode ||
      cur.playing != last.playing) {
    return true;
  }
  final since = now.difference(lastAt);
  if (since >= partyStateEvery) return true;
  final expected =
      last.position + (last.playing ? since.inMilliseconds / 1000 : 0);
  return (cur.position - expected).abs() > 1.5;
}

/// The wait before reconnect attempt [n] (from 0): 1 s, doubling, 30 s at most.
Duration partyBackoff(int n) =>
    n > 4 ? const Duration(seconds: 30) : Duration(seconds: 1 << max(0, n));

/// Estimates the bot's clock from ping round trips, keeping the sample with
/// the shortest trip.
class PartyClock {
  int offset = 0, _rtt = 0;
  bool _ok = false;
  void sample(int sentLocal, int recvLocal, int server) {
    final rtt = recvLocal - sentLocal;
    if (rtt < 0 || rtt > 10000 || _ok && rtt > _rtt) return;
    offset = server - (sentLocal + rtt ~/ 2);
    _rtt = rtt;
    _ok = true;
  }

  int serverNow(int local) => local + offset;
}

final _partyId = RegExp(r'^\d{1,25}$');

/// The party id from what a member pastes: the id, the Join in Otakase link
/// (…/open/?party=ID), an otakase://party/ID link or the Discord event link.
String? parsePartyId(String input) {
  final s = input.trim();
  if (_partyId.hasMatch(s)) return s;
  final u = Uri.tryParse(s);
  if (u == null) return null;
  final q = u.queryParameters['party'] ?? '';
  if (_partyId.hasMatch(q)) return q;
  final parts = '${u.host}${u.path}'
      .split('/')
      .where((p) => p.isNotEmpty)
      .toList();
  if (parts.length >= 2 && _partyId.hasMatch(parts.last)) return parts.last;
  return null;
}

/// The WebSocket address of party [id] on the bot at [base].
Uri partySocketUrl(String base, String id) {
  final u = Uri.parse(base.replaceAll(RegExp(r'/+$'), ''));
  return u.replace(
    scheme: u.scheme == 'http' ? 'ws' : 'wss',
    path: '${u.path}/v1/party/${Uri.encodeComponent(id)}/ws',
  );
}

/// A planned party as the bot lists it (GET /v1/parties).
class PlannedParty {
  PlannedParty.fromJson(Json j)
    : id = j['id']?.toString() ?? '',
      title = j['title'] as String? ?? 'Watch party',
      start = DateTime.tryParse(j['start'] as String? ?? '')?.toLocal(),
      anilistId = number(j['anilist_id']),
      sync = j['sync'] == true,
      host = j['host'] as String? ?? '',
      going = number(j['going']),
      rsvp = j['rsvp'] == true,
      live = j['live'] == true,
      joinUrl = j['join_url'] as String? ?? '',
      eventUrl = j['event_url'] as String? ?? '';
  final String id, title, host, joinUrl, eventUrl;
  final DateTime? start;
  final int anilistId, going;
  final bool sync, rsvp, live;
}

/// When a party starts, said for a list row.
String partyStartLabel(DateTime start, DateTime now) {
  String hm(DateTime t) =>
      '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
  if (!start.isAfter(now)) return 'started ${hm(start)}';
  final days = DateTime(
    start.year,
    start.month,
    start.day,
  ).difference(DateTime(now.year, now.month, now.day)).inDays;
  if (days == 0) return 'today ${hm(start)}';
  if (days == 1) return 'tomorrow ${hm(start)}';
  const week = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
  const months = [
    'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', //
    'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
  ];
  return '${week[start.weekday - 1]} ${start.day} ${months[start.month - 1]} ${hm(start)}';
}

/// One open connection to the bot.
abstract class PartySocket {
  Stream<dynamic> get messages;
  void send(String text);
  Future<void> close();
}

class _IOPartySocket implements PartySocket {
  _IOPartySocket(this.ws);
  final WebSocket ws;
  @override
  Stream<dynamic> get messages => ws;
  @override
  void send(String text) => ws.add(text);
  @override
  Future<void> close() => ws.close(WebSocketStatus.normalClosure);
}

/// Why a party connection stopped for good.
class PartyGone implements Exception {
  const PartyGone(this.message);
  final String message;
  @override
  String toString() => message;
}

typedef PartyConnect = Future<PartySocket> Function(
  Uri url,
  Map<String, String> headers,
);

Future<PartySocket> _ioConnect(Uri url, Map<String, String> headers) async {
  final ws = await WebSocket.connect(
    url.toString(),
    headers: headers,
  ).timeout(const Duration(seconds: 15));
  ws.pingInterval = const Duration(seconds: 30);
  return _IOPartySocket(ws);
}

class PartyLine {
  const PartyLine(this.name, this.text, this.at);
  final String name, text;
  final DateTime at;
}

/// Watch parties: the planned list, RSVPs, joining a synced one, hosting
/// (Watch with me) and following the host's player.
class WatchParties extends ChangeNotifier {
  WatchParties({
    required this.discord,
    required this.store,
    PartyConnect? connect,
    DateTime Function()? now,
  }) : connect = connect ?? _ioConnect,
       now = now ?? DateTime.now;
  final DiscordService discord;
  final AppStore store;
  final PartyConnect connect;
  final DateTime Function() now;

  /// The playing episode, from the player (see PlayerPage.remoteStatus).
  Json? Function() status = () => null;

  /// Runs a player command (pause, play, seek_to, episode, party_skip, next).
  Future<void> Function(String action, Json args) command = (_, _) async {};

  /// Opens the host's show and episode in the player.
  Future<void> Function(int anilistId, int episode)? open;

  String? id, show, joinUrl, lastError;
  bool hosting = false, watchWithMe = false, connected = false;
  int watching = 0;
  List<Json> members = [];
  final lines = <PartyLine>[];
  String note = '';
  DateTime? noteAt;
  ({String what, int votes, int needed})? votes;
  PartyHostState? host;
  final clock = PartyClock();
  PartySocket? _socket;
  StreamSubscription<dynamic>? _sub;
  Timer? _tick, _ping, _reconnect;
  int _epoch = 0, _attempt = 0;
  PartyHostState? _sent;
  DateTime _sentAt = DateTime.fromMillisecondsSinceEpoch(0);
  DateTime _seekAt = DateTime.fromMillisecondsSinceEpoch(0);
  DateTime? _openingAt, _lastPlayback;
  bool _starting = false, closed = false;

  bool get active => id != null;

  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  Future<List<PlannedParty>> planned() async {
    final r = await discord.request('GET', '/v1/parties');
    return (r['parties'] as List? ?? [])
        .whereType<Map>()
        .map((m) => PlannedParty.fromJson(m.cast<String, dynamic>()))
        .where((p) => p.id.isNotEmpty)
        .toList();
  }

  Future<({bool going, int count})> rsvp(String partyId, bool going) async {
    final r = await discord.request(
      'POST',
      '/v1/party/${Uri.encodeComponent(partyId)}/rsvp',
      body: {'going': going},
    );
    return (going: r['going'] == true, count: number(r['count']));
  }

  void _say(String text) {
    note = text;
    noteAt = now();
    notifyListeners();
  }

  /// Joins the party [input] names. Throws a [PartyGone] when it can't.
  Future<void> join(String input) async {
    final partyId = parsePartyId(input);
    if (partyId == null) {
      throw PartyGone('“${input.trim()}” isn’t a watch party link or id.');
    }
    if (!discord.linked) {
      throw const PartyGone('Link this profile to Discord first.');
    }
    if (id == partyId) return;
    await leave();
    final epoch = ++_epoch;
    final welcome = await _dial(partyId);
    if (epoch != _epoch) return;
    id = partyId;
    _begin(welcome);
  }

  Future<Json> _dial(String partyId) async {
    final url = partySocketUrl(discord.baseUrl, partyId);
    final PartySocket socket;
    try {
      socket = await connect(url, {
        'Authorization': 'Bearer ${discord.linkToken}',
        'User-Agent': 'Otakase/$appVersion',
      });
    } catch (_) {
      throw await _whyRefused(partyId);
    }
    final first = Completer<Json>();
    final sub = socket.messages.listen(
      (m) {
        if (first.isCompleted) return _handle(m);
        try {
          final j = (jsonDecode(m as String) as Map).cast<String, dynamic>();
          if (j['type'] == 'welcome') {
            first.complete(j);
          } else {
            first.completeError(const PartyGone('The bot answered oddly.'));
          }
        } catch (_) {
          first.completeError(const PartyGone('The bot answered oddly.'));
        }
      },
      onDone: () {
        if (!first.isCompleted) {
          first.completeError(const PartyGone('The bot closed the party.'));
        } else {
          _lost(socket);
        }
      },
      onError: (Object _) {
        if (!first.isCompleted) {
          first.completeError(const PartyGone('Lost the bot.'));
        }
      },
      cancelOnError: true,
    );
    try {
      final welcome = await first.future.timeout(const Duration(seconds: 15));
      await _sub?.cancel();
      _sub = sub;
      _socket = socket;
      connected = true;
      _send({'type': 'hello', 'version': appVersion});
      _pingNow();
      return welcome;
    } catch (e) {
      await sub.cancel();
      await socket.close();
      if (e is PartyGone) rethrow;
      throw const PartyGone('No welcome from the bot.');
    }
  }

  /// A refused upgrade does not say why; asking plainly does.
  Future<PartyGone> _whyRefused(String partyId) async {
    try {
      await discord.request(
        'GET',
        '/v1/party/${Uri.encodeComponent(partyId)}/ws',
        revokeOnUnauthorized: false,
      );
    } on BotFailure catch (e) {
      return switch (e.status) {
        401 => const PartyGone(
          'The bot doesn’t know this profile. Link Discord again.',
        ),
        404 => const PartyGone(
          'There’s no synced party with that id. It may be over, or planned without sync.',
        ),
        403 => const PartyGone('This watch party is full.'),
        _ => PartyGone('The bot answered ${e.status}.'),
      };
    } catch (_) {}
    return const PartyGone('Couldn’t reach the Discord bot.');
  }

  void _begin(Json welcome) {
    hosting = welcome['host'] == true;
    show = (welcome['show'] as String? ?? '').trim();
    if (show!.isEmpty) show = 'the watch party';
    _sent = null;
    _attempt = 0;
    _tick?.cancel();
    _tick = Timer.periodic(
      Duration(milliseconds: hosting ? 1000 : 500),
      (_) => unawaited(hosting ? hostTick() : memberTick()),
    );
    _ping?.cancel();
    _ping = Timer.periodic(const Duration(seconds: 20), (_) => _pingNow());
    _say(
      hosting
          ? 'You’re hosting $show. Everyone follows your player.'
          : 'Joined $show. Waiting for the host…',
    );
  }

  void _pingNow() => _send({'type': 'ping', 't': now().millisecondsSinceEpoch});

  void _send(Json v) {
    try {
      _socket?.send(jsonEncode(v));
    } catch (_) {
      /* Dropped while disconnected, as in the CLI. */
    }
  }

  void _handle(dynamic raw) {
    Json m;
    try {
      m = (jsonDecode(raw as String) as Map).cast<String, dynamic>();
    } catch (_) {
      return;
    }
    switch (m['type']) {
      case 'pong':
        clock.sample(
          number(m['t']),
          now().millisecondsSinceEpoch,
          number(m['server_at']),
        );
      case 'state':
        host = PartyHostState.fromJson(m);
        if (!hosting) unawaited(memberTick());
      case 'members':
        members = (m['list'] as List? ?? [])
            .whereType<Map>()
            .map((e) => e.cast<String, dynamic>())
            .toList();
        final n = max(0, members.length - 1);
        if (hosting && n != watching) _say('Watch party: $n watching along');
        watching = n;
        notifyListeners();
      case 'chat':
        _line(m['name'] as String? ?? '', m['text'] as String? ?? '');
      case 'react':
        _line(m['name'] as String? ?? '', m['emoji'] as String? ?? '');
      case 'votes':
        votes = (
          what: m['what'] as String? ?? '',
          votes: number(m['votes']),
          needed: number(m['needed']),
        );
        _say(
          '${votes!.votes} of ${votes!.needed} want to ${_voteWords(votes!.what)}',
        );
      case 'voted':
        votes = null;
        final what = m['what'] as String? ?? '';
        _say('The party voted to ${_voteWords(what)}');
        if (hosting) {
          unawaited(command(what == 'next' ? 'next' : 'party_skip', {}));
        }
      case 'ended':
        _say('The watch party ended');
        unawaited(_finish());
    }
  }

  static String _voteWords(String what) =>
      what == 'next' ? 'play the next episode' : 'skip ahead';

  void _line(String name, String text) {
    if (text.trim().isEmpty) return;
    lines.add(PartyLine(clipRunes(name, 32), clipRunes(text, 200), now()));
    while (lines.length > 6) {
      lines.removeAt(0);
    }
    notifyListeners();
  }

  /// Visible chat lines: the last few from the past 12 seconds.
  List<PartyLine> recentLines() => lines
      .where((l) => now().difference(l.at) < const Duration(seconds: 12))
      .toList();

  void vote(String what) {
    if (!active) return;
    _send({'type': 'vote', 'what': what});
    _say('You voted to ${_voteWords(what)}');
  }

  void _lost(PartySocket socket) {
    if (!identical(socket, _socket) || id == null) return;
    connected = false;
    _socket = null;
    notifyListeners();
    final partyId = id!, epoch = _epoch;
    _reconnect?.cancel();
    _reconnect = Timer(partyBackoff(_attempt++), () async {
      if (epoch != _epoch) return;
      try {
        await _dial(partyId);
        _sent = null; // the host resends straight away
        if (!hosting) _readyNow();
        notifyListeners();
      } on PartyGone catch (e) {
        if (epoch != _epoch) return;
        if (e.message.contains('Link') ||
            e.message.contains('no synced') ||
            e.message.contains('full')) {
          lastError = e.message;
          _say('Left the watch party: ${e.message}');
          await _finish();
        } else {
          _lost(socket);
        }
      } catch (_) {
        if (epoch == _epoch) _lost(socket);
      }
    });
  }

  void _readyNow() {
    final s = status();
    if (s != null && number(s['episode']) > 0) {
      _send({'type': 'ready', 'episode': number(s['episode'])});
    }
  }

  /// The host's side: send the player state on changes and every few seconds.
  Future<void> hostTick() async {
    if (!hosting || !active) return;
    final s = status();
    if (s != null && s['has_player'] == true) _lastPlayback = now();
    if (watchWithMe &&
        (!store.flag('DiscordWatchWithMe') ||
            _lastPlayback != null &&
                now().difference(_lastPlayback!) > watchWithMeIdle)) {
      _send({'type': 'end'});
      await leave();
      return;
    }
    var cur = PartyHostState(
      anilistId: number(s?['anilist_id']),
      episode: number(s?['episode']),
      playing: false,
      position: _sent?.position ?? 0,
    );
    if (s != null && s['loaded'] == true) {
      cur = PartyHostState(
        anilistId: cur.anilistId,
        episode: cur.episode,
        playing: s['playing'] == true,
        position: _seconds(s),
      );
    }
    final at = now();
    if (!partyShouldSend(_sent, _sentAt, cur, at)) return;
    final sent = PartyHostState(
      anilistId: cur.anilistId,
      episode: cur.episode,
      playing: cur.playing,
      position: cur.position,
      sentAt: clock.serverNow(at.millisecondsSinceEpoch),
    );
    _send(sent.toJson());
    _sent = sent;
    _sentAt = at;
  }

  static double _seconds(Json s) => s['position_ms'] is num
      ? (s['position_ms'] as num) / 1000
      : number(s['position']).toDouble();

  /// A member's side: keep the player on the host's state.
  Future<void> memberTick() async {
    final h = host;
    if (hosting || !active || h == null) return;
    final s = status();
    final local = s == null || s['has_player'] != true
        ? null
        : PartyLocal(
            anilistId: number(s['anilist_id']),
            episode: number(s['episode']),
            loaded: s['loaded'] == true,
            paused: s['playing'] != true,
            position: _seconds(s),
            sinceSeek: now().difference(_seekAt),
          );
    final a = decidePartySync(
      h,
      local,
      clock.serverNow(now().millisecondsSinceEpoch),
    );
    if (a.open) {
      // Opening takes a while (finding a source); don't stack opens.
      if (_openingAt != null &&
          now().difference(_openingAt!) < const Duration(seconds: 90)) {
        return;
      }
      _openingAt = now();
      _say(a.note);
      if (local != null) await command('stop', {});
      await open?.call(h.anilistId, h.episode);
      return;
    }
    if (local != null && local.anilistId == h.anilistId) _openingAt = null;
    if (a.switchEpisode) {
      if (_openingAt != null &&
          now().difference(_openingAt!) < const Duration(seconds: 90)) {
        return;
      }
      _openingAt = now();
      _say(a.note);
      await command('episode', {'episode': h.episode});
      return;
    }
    if (local != null && local.loaded) {
      _openingAt = null;
      if (_readyFor != local.episode) {
        _readyFor = local.episode;
        _send({'type': 'ready', 'episode': local.episode});
      }
    }
    if (a.pause) await command('pause', {});
    if (a.play) await command('play', {});
    if (a.seek) {
      _seekAt = now();
      await command('seek_to', {'position': a.seekTo});
    }
    if (a.note.isNotEmpty) _say(a.note);
  }

  int _readyFor = 0;

  /// Watch with me: called while something plays. Hosts a party of its own
  /// when the setting is on, for others to join from Discord.
  Future<void> playing(Anime anime, int episode) async {
    _lastPlayback = now();
    if (!store.flag('DiscordWatchWithMe') ||
        !discord.linked ||
        active ||
        _starting ||
        anime.id <= 0 ||
        anime.isAdult) {
      return;
    }
    _starting = true;
    try {
      final r = await discord.request(
        'POST',
        '/v1/party/start',
        body: {
          'anilist_id': anime.id,
          'episode': episode,
          'title': anime.title,
        },
      );
      final partyId = parsePartyId(r['id']?.toString() ?? '');
      final url = r['join_url'] as String? ?? '';
      if (partyId == null || url.isEmpty) {
        throw const PartyGone('The bot answered without a party.');
      }
      final epoch = ++_epoch;
      final welcome = await _dial(partyId);
      if (epoch != _epoch) return;
      if (welcome['host'] != true) {
        await _socket?.close();
        throw const PartyGone('The bot didn’t make this app the host.');
      }
      id = partyId;
      joinUrl = url;
      watchWithMe = true;
      _begin(welcome);
    } catch (e) {
      lastError = e.toString();
      notifyListeners();
    } finally {
      _starting = false;
    }
  }

  /// Ends a Watch with me party for everyone; leaves any other party.
  Future<void> leave() async {
    if (watchWithMe && hosting) _send({'type': 'end'});
    await _finish();
  }

  Future<void> _finish() async {
    _epoch++;
    _tick?.cancel();
    _ping?.cancel();
    _reconnect?.cancel();
    final socket = _socket;
    _socket = null;
    await _sub?.cancel();
    _sub = null;
    await socket?.close();
    id = null;
    show = null;
    joinUrl = null;
    hosting = false;
    watchWithMe = false;
    connected = false;
    host = null;
    members = [];
    watching = 0;
    votes = null;
    _openingAt = null;
    _readyFor = 0;
    notifyListeners();
  }

  Future<void> close() async {
    await leave();
    closed = true;
    super.dispose();
  }
}
