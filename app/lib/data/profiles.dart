import 'dart:async';
import 'dart:convert';
import 'dart:isolate';
import 'dart:math';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

import '../core.dart';
import '../services.dart';
import 'auth.dart';
import 'catalog.dart';
import 'models.dart';
import 'store.dart';

const profileColors = [
  0xffd2492f,
  0xff9bb67a,
  0xff7fa2c4,
  0xffe3b45a,
  0xffb892ba,
  0xffb9b0a4,
];
String _randomHex(int length) => List.generate(
  length,
  (_) => Random.secure().nextInt(256).toRadixString(16).padLeft(2, '0'),
).join();

Future<String> deriveProfileLock(String salt, String code) => Isolate.run(() {
  final hmac = Hmac(sha256, utf8.encode(code));
  var u = hmac.convert([...utf8.encode(salt), 0, 0, 0, 1]).bytes;
  final result = List<int>.from(u);
  for (var i = 1; i < 100000; i++) {
    u = hmac.convert(u).bytes;
    for (var j = 0; j < result.length; j++) {
      result[j] ^= u[j];
    }
  }
  return base64UrlEncode(result);
});
bool _sameHash(String a, String b) {
  if (a.length != b.length) return false;
  var difference = 0;
  for (var i = 0; i < a.length; i++) {
    difference |= a.codeUnitAt(i) ^ b.codeUnitAt(i);
  }
  return difference == 0;
}

bool validDirectionCode(String code) => RegExp(r'^[UDLR]{4,8}$').hasMatch(code);

class Profile {
  const Profile({
    required this.id,
    required this.name,
    this.color = 0xffd2492f,
    this.avatar = '',
    this.lockHash = '',
    this.lockSalt = '',
    this.failures = 0,
    this.lockUntil = 0,
  });
  final String id, name, avatar, lockHash, lockSalt;
  final int color, failures, lockUntil;
  bool get locked => lockHash.isNotEmpty;
  Profile copyWith({
    String? name,
    int? color,
    String? avatar,
    String? lockHash,
    String? lockSalt,
    int? failures,
    int? lockUntil,
  }) => Profile(
    id: id,
    name: name ?? this.name,
    color: color ?? this.color,
    avatar: avatar ?? this.avatar,
    lockHash: lockHash ?? this.lockHash,
    lockSalt: lockSalt ?? this.lockSalt,
    failures: failures ?? this.failures,
    lockUntil: lockUntil ?? this.lockUntil,
  );
  Json toJson() => {
    'id': id,
    'name': name,
    'color': color,
    'avatar': avatar,
    'lockHash': lockHash,
    'lockSalt': lockSalt,
    'failures': failures,
    'lockUntil': lockUntil,
  };
  factory Profile.fromJson(Json j) => Profile(
    id: j['id'] as String,
    name: j['name'] as String,
    color: number(j['color']),
    avatar: j['avatar'] as String? ?? '',
    lockHash: j['lockHash'] as String? ?? '',
    lockSalt: j['lockSalt'] as String? ?? '',
    failures: number(j['failures']),
    lockUntil: number(j['lockUntil']),
  );
}

class ProfileUnlock {
  const ProfileUnlock._(this.id, this.nonce, this.expires);
  final String id, nonce;
  final DateTime expires;
}

class ProfileLocked implements Exception {
  const ProfileLocked();
  @override
  String toString() => 'Enter this profile’s direction code.';
}

class ProfileTokens implements TokenStore {
  ProfileTokens(this.base, this.preferences, this.profileId);
  final TokenStore base;
  final SharedPreferences preferences;
  final String profileId;
  bool active = true;
  Future<void> _writing = Future<void>.value();
  String get prefix => 'profile.$profileId.';
  @override
  Future<String?> read(String key) async {
    if (!active) return null;
    final value = await base.read('$prefix$key');
    return active ? value : null;
  }

  @override
  Future<void> write(String key, String? value) =>
      _writing = _writing.catchError((Object _) {}).then((_) async {
        if (!active) return;
        final keys = {
          ...preferences.getStringList('${prefix}secure.keys') ?? <String>[],
        };
        if (value != null) {
          keys.add(key);
          await preferences.setStringList(
            '${prefix}secure.keys',
            keys.toList()..sort(),
          );
        }
        await base.write('$prefix$key', value);
        if (value == null) keys.remove(key);
        await preferences.setStringList(
          '${prefix}secure.keys',
          keys.toList()..sort(),
        );
      });
  Future<void> deactivate() async {
    active = false;
    await _writing.catchError((Object _) {});
  }

  Future<void> clear() async {
    final keys = {
      ...preferences.getStringList('${prefix}secure.keys') ?? <String>[],
      'account.AniList',
      'account.MAL',
      'login.pending',
      'discord.link',
    };
    for (final key in keys) {
      await base.write('$prefix$key', null);
    }
    await preferences.remove('${prefix}secure.keys');
  }
}

class ProfileManager extends ChangeNotifier {
  ProfileManager({
    required this.preferences,
    required this.tokens,
    required this.core,
    required this.isTV,
    http.Client Function()? clientFactory,
    DateTime Function()? now,
    Future<String> Function(String, String)? hashLock,
    this.platformCallbacks = true,
  }) : clientFactory = clientFactory ?? http.Client.new,
       now = now ?? DateTime.now,
       hashLock = hashLock ?? deriveProfileLock;
  final SharedPreferences preferences;
  final TokenStore tokens;
  final CoreClient core;
  final bool isTV, platformCallbacks;
  final http.Client Function() clientFactory;
  final DateTime Function() now;
  final Future<String> Function(String, String) hashLock;
  final _profiles = <Profile>[];
  final _unlocks = <String, String>{};
  List<Profile> get profiles => List.unmodifiable(_profiles);
  String activeId = '1';
  AppServices? session;
  int generation = 0;
  bool busy = false, closed = false;
  bool initialized = false;
  bool _disposed = false;
  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  String? error;
  bool get askOnStart => preferences.getBool('profiles.askOnStart') ?? isTV;
  Profile get active => _profiles.firstWhere(
    (p) => p.id == activeId,
    orElse: () => _profiles.first,
  );
  Profile profile(String id) => _profiles.firstWhere((p) => p.id == id);
  Future<void> initialize() async {
    if (session != null) return;
    _profiles.clear();
    error = null;
    final raw = preferences.getString('profiles.v1');
    if (raw != null) {
      final data = (jsonDecode(raw) as Map).cast<String, dynamic>();
      final seen = <String>{};
      for (final row in data['profiles'] as List) {
        final p = Profile.fromJson((row as Map).cast<String, dynamic>());
        if (seen.add(p.id) && _profiles.length < 6) _profiles.add(p);
      }
      activeId = data['active'] as String? ?? '1';
    }
    if (_profiles.isEmpty) {
      if (raw != null) throw StateError('The saved profile list is empty.');
      _profiles.add(const Profile(id: '1', name: 'Profile 1'));
      activeId = '1';
      await _migrate();
    }
    initialized = true;
    if (!_profiles.any((p) => p.id == activeId)) activeId = _profiles.first.id;
    if (!(askOnStart && _profiles.length > 1) && !active.locked) {
      await switchTo(activeId);
    }
    notifyListeners();
  }

  Future<void> _migrate() async {
    final scoped = ProfileTokens(tokens, preferences, '1');
    for (final key in ['settings.v1', 'history.v1']) {
      final value = preferences.getString(key);
      if (value != null && !preferences.containsKey('profile.1.$key')) {
        if (!await preferences.setString('profile.1.$key', value)) {
          throw StateError('Unable to migrate profile settings.');
        }
      }
    }
    for (final key in [
      'account.AniList',
      'account.MAL',
      'login.pending',
      'discord.link',
    ]) {
      final value = await tokens.read(key);
      if (value != null && await scoped.read(key) == null) {
        await scoped.write(key, value);
      }
    }
    await _save();
    for (final key in ['settings.v1', 'history.v1']) {
      await preferences.remove(key);
    }
    for (final key in [
      'account.AniList',
      'account.MAL',
      'login.pending',
      'discord.link',
    ]) {
      await tokens.write(key, null);
    }
  }

  Future<void> _save() async {
    final saved = await preferences.setString(
      'profiles.v1',
      jsonEncode({
        'active': activeId,
        'profiles': _profiles.map((p) => p.toJson()).toList(),
      }),
    );
    if (!saved) throw StateError('Unable to save profiles.');
  }

  void _replace(Profile p) {
    _profiles[_profiles.indexWhere((v) => v.id == p.id)] = p;
  }

  Future<Profile> add(String name, {int? color}) async {
    if (_profiles.length >= 6) throw StateError('You have six profiles.');
    final p = Profile(
      id: _randomHex(8),
      name: _name(name),
      color: color ?? profileColors[_profiles.length],
    );
    _profiles.add(p);
    await _save();
    notifyListeners();
    return p;
  }

  String _name(String name) {
    final trimmed = name.trim();
    if (trimmed.isEmpty || trimmed.runes.length > 32) {
      throw ArgumentError('Use a profile name with 1–32 characters.');
    }
    return trimmed;
  }

  Future<void> edit(
    String id, {
    required String name,
    required int color,
    ProfileUnlock? unlock,
  }) async {
    final p = profile(id);
    _authorize(p, unlock, allowCurrent: true);
    _replace(p.copyWith(name: _name(name), color: color));
    await _save();
    notifyListeners();
  }

  int waitSeconds(String id) => max<int>(
    0,
    ((profile(id).lockUntil - now().millisecondsSinceEpoch) / 1000).ceil(),
  );
  Future<ProfileUnlock?> unlock(String id, String code) async {
    final p = profile(id);
    if (waitSeconds(id) > 0) return null;
    if (!p.locked) return _permit(id);
    final matched =
        validDirectionCode(code) &&
        _sameHash(await hashLock(p.lockSalt, code), p.lockHash);
    if (matched) {
      _replace(p.copyWith(failures: 0, lockUntil: 0));
      await _save();
      notifyListeners();
      return _permit(id);
    }
    final failures = p.failures >= 5 ? 1 : p.failures + 1;
    _replace(
      p.copyWith(
        failures: failures,
        lockUntil: failures >= 5
            ? now().add(const Duration(seconds: 30)).millisecondsSinceEpoch
            : 0,
      ),
    );
    await _save();
    notifyListeners();
    return null;
  }

  ProfileUnlock _permit(String id) {
    final nonce = _randomHex(16);
    _unlocks[id] = nonce;
    return ProfileUnlock._(id, nonce, now().add(const Duration(minutes: 1)));
  }

  void _authorize(
    Profile p,
    ProfileUnlock? unlock, {
    bool allowCurrent = false,
  }) {
    if (!p.locked || allowCurrent && activeId == p.id && session != null) {
      return;
    }
    if (unlock == null ||
        unlock.id != p.id ||
        !unlock.expires.isAfter(now()) ||
        _unlocks.remove(p.id) != unlock.nonce) {
      throw const ProfileLocked();
    }
  }

  Future<void> setLock(String id, String code, {ProfileUnlock? unlock}) async {
    if (!validDirectionCode(code)) throw ArgumentError('Use 4–8 directions.');
    final p = profile(id);
    _authorize(p, unlock, allowCurrent: true);
    final salt = _randomHex(24);
    final hash = await hashLock(salt, code);
    _replace(
      profile(id)
          .copyWith(lockSalt: salt, lockHash: hash, failures: 0, lockUntil: 0),
    );
    _unlocks.remove(id);
    await _save();
    notifyListeners();
  }

  Future<void> removeLock(String id, {ProfileUnlock? unlock}) async {
    final p = profile(id);
    _authorize(p, unlock);
    _replace(p.copyWith(lockHash: '', lockSalt: '', failures: 0, lockUntil: 0));
    _unlocks.remove(id);
    await _save();
    notifyListeners();
  }

  Future<void> switchTo(String id, {ProfileUnlock? unlock}) async {
    if (busy) return;
    final target = profile(id);
    if (session != null && activeId == id) return;
    _authorize(target, unlock);
    busy = true;
    error = null;
    notifyListeners();
    try {
      final previous = session;
      if (previous != null) {
        previous.auth.removeListener(_avatarChanged);
        await previous.close();
      }
      session = null;
      activeId = id;
      final client = clientFactory();
      final auth = AuthService(
        client: client,
        storage: ProfileTokens(tokens, preferences, id),
      );
      final services = AppServices(
        catalog: Catalog(client: client),
        store: AppStore(preferences, namespace: 'profile.$id.'),
        core: core,
        auth: auth,
        profiles: this,
        profileId: id,
      );
      await auth.initialize(platformCallbacks: platformCallbacks);
      if (closed) {
        await services.close();
        return;
      }
      session = services;
      generation++;
      auth.addListener(_avatarChanged);
      _avatarChanged();
      await _save();
    } catch (e) {
      error = e.toString();
      rethrow;
    } finally {
      busy = false;
      if (!closed) notifyListeners();
    }
  }

  void _avatarChanged() {
    final auth = session?.auth;
    if (auth == null) return;
    final avatar =
        ['AniList', 'MAL']
            .map((s) => auth.account(s)?.avatar ?? '')
            .where((s) => s.isNotEmpty)
            .firstOrNull ??
        '';
    final p = active;
    if (p.avatar == avatar) return;
    _replace(p.copyWith(avatar: avatar));
    unawaited(_save());
    if (!closed) notifyListeners();
  }

  Future<void> setAskOnStart(bool value) async {
    await preferences.setBool('profiles.askOnStart', value);
    notifyListeners();
  }

  Future<void> delete(String id, {ProfileUnlock? unlock}) async {
    if (_profiles.length == 1) throw StateError('Keep at least one profile.');
    final p = profile(id);
    _authorize(p, unlock, allowCurrent: true);
    if (activeId == id) {
      final previous = session;
      if (previous != null) {
        previous.auth.removeListener(_avatarChanged);
        await previous.close();
      }
      session = null;
    }
    await ProfileTokens(tokens, preferences, id).clear();
    for (final key
        in preferences
            .getKeys()
            .where((key) => key.startsWith('profile.$id.'))
            .toList()) {
      await preferences.remove(key);
    }
    _profiles.removeWhere((p) => p.id == id);
    _unlocks.remove(id);
    if (activeId == id) activeId = _profiles.first.id;
    await _save();
    if (!closed) notifyListeners();
  }

  Future<void>? _closing;
  Future<void> close() => _closing ??= _close();
  Future<void> _close() async {
    closed = true;
    await session?.close();
    session = null;
    dispose();
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    closed = true;
    unawaited(session?.close() ?? Future<void>.value());
    super.dispose();
  }
}
