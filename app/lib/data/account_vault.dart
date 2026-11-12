import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:cryptography/cryptography.dart';
import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'account.dart';
import 'auth.dart';
import 'models.dart';

// The token vault: AniList, MAL and Trakt logins travel between a person's
// devices without the server being able to read them.
//
// Each account has one vault key. Every device has its own X25519 key pair;
// the vault key is stored on the server wrapped for each device's public
// key. A new device gets the vault key when a device that already has it
// approves it in Devices. Each profile's logins are sealed with the vault
// key (AES-GCM) into one blob.

final _x25519 = X25519();
final _aes = AesGcm.with256bits();
final _hkdf = Hkdf(hmac: Hmac.sha256(), outputLength: 32);
final _wrapInfo = utf8.encode('otakase vault wrap v1');

String b64url(List<int> bytes) => base64UrlEncode(bytes).replaceAll('=', '');
Uint8List unb64url(String text) =>
    base64Url.decode(text.padRight((text.length + 3) ~/ 4 * 4, '='));

/// Device key pairs kept in the token store, one per account on this
/// device.
class TokenStoreKeys implements MovableKeys {
  TokenStoreKeys(this.tokens);
  final TokenStore tokens;
  String _key(String name) => 'otakase.devicekey.$name';

  Future<SimpleKeyPair> pair(String name) async {
    final saved = await tokens.read(_key(name));
    if (saved != null && saved.isNotEmpty) {
      return _x25519.newKeyPairFromSeed(unb64url(saved));
    }
    final seed = List<int>.generate(32, (_) => Random.secure().nextInt(256));
    await tokens.write(_key(name), b64url(seed));
    return _x25519.newKeyPairFromSeed(seed);
  }

  @override
  Future<String> publicKey(String name) async =>
      b64url((await (await pair(name)).extractPublicKey()).bytes);

  @override
  Future<void> move(String from, String to) async {
    final value = await tokens.read(_key(from));
    if (value == null) return;
    await tokens.write(_key(to), value);
    await tokens.write(_key(from), null);
  }

  Future<void> forget(String name) => tokens.write(_key(name), null);

  String _vaultName(String account) => 'otakase.vaultkey.$account';

  /// The vault key this device last held for [account], kept after signing
  /// out so signing in again needs no approval.
  Future<(int, List<int>)?> savedVault(String account) async {
    final saved = await tokens.read(_vaultName(account));
    if (saved == null || saved.isEmpty) return null;
    final (version, key) = switch (saved.split('.')) {
      [final v, final k] => (int.tryParse(v), k),
      _ => (null, ''),
    };
    if (version == null || key.isEmpty) return null;
    try {
      return (version, unb64url(key).toList());
    } catch (_) {
      return null;
    }
  }

  Future<void> saveVault(String account, int version, List<int> key) =>
      tokens.write(_vaultName(account), '$version.${b64url(key)}');

  Future<void> forgetVault(String account) =>
      tokens.write(_vaultName(account), null);
}

final _checkInfo = utf8.encode('otakase vault check v1');

/// A keyed hash of the vault key. The server keeps it so a device can prove
/// it still has the key, without the server learning the key.
Future<String> vaultCheck(List<int> vaultKey) async {
  final mac = await Hmac.sha256().calculateMac(
    _checkInfo,
    secretKey: SecretKey(vaultKey),
  );
  return b64url(mac.bytes);
}

/// Wraps [vaultKey] for the device with [publicKey]: a fresh key pair, a
/// shared secret through X25519, HKDF, then AES-GCM.
Future<String> wrapKey(List<int> vaultKey, String publicKey) async {
  final eph = await _x25519.newKeyPair();
  final ephPublic = await eph.extractPublicKey();
  final shared = await _x25519.sharedSecretKey(
    keyPair: eph,
    remotePublicKey: SimplePublicKey(
      unb64url(publicKey),
      type: KeyPairType.x25519,
    ),
  );
  final kek = await _hkdf.deriveKey(
    secretKey: shared,
    info: _wrapInfo,
    nonce: ephPublic.bytes,
  );
  final box = await _aes.encrypt(vaultKey, secretKey: kek);
  return b64url([
    ...ephPublic.bytes,
    ...box.nonce,
    ...box.cipherText,
    ...box.mac.bytes,
  ]);
}

Future<List<int>> unwrapKey(String wrapped, SimpleKeyPair mine) async {
  final bytes = unb64url(wrapped);
  if (bytes.length != 32 + 12 + 32 + 16) {
    throw const AccountFailure('The vault key is damaged.');
  }
  final ephPublic = bytes.sublist(0, 32);
  final shared = await _x25519.sharedSecretKey(
    keyPair: mine,
    remotePublicKey: SimplePublicKey(ephPublic, type: KeyPairType.x25519),
  );
  final kek = await _hkdf.deriveKey(
    secretKey: shared,
    info: _wrapInfo,
    nonce: ephPublic,
  );
  return _aes.decrypt(
    SecretBox(
      bytes.sublist(44, 76),
      nonce: bytes.sublist(32, 44),
      mac: Mac(bytes.sublist(76)),
    ),
    secretKey: kek,
  );
}

Future<String> seal(List<int> vaultKey, Json value) async {
  final box = await _aes.encrypt(
    utf8.encode(jsonEncode(value)),
    secretKey: SecretKey(vaultKey),
  );
  return b64url([...box.nonce, ...box.cipherText, ...box.mac.bytes]);
}

Future<Json> open(List<int> vaultKey, String sealed) async {
  final bytes = unb64url(sealed);
  if (bytes.length < 28) throw const AccountFailure('The vault is damaged.');
  final plain = await _aes.decrypt(
    SecretBox(
      bytes.sublist(12, bytes.length - 16),
      nonce: bytes.sublist(0, 12),
      mac: Mac(bytes.sublist(bytes.length - 16)),
    ),
    secretKey: SecretKey(vaultKey),
  );
  return (jsonDecode(utf8.decode(plain)) as Map).cast<String, dynamic>();
}

/// The login keys a vault blob carries for each profile.
const vaultLogins = ['account.AniList', 'account.MAL', 'trakt_token'];

/// A device waiting for the vault key, shown in Devices.
class WaitingDevice {
  const WaitingDevice(this.id, this.name, this.publicKey);
  final String id, name, publicKey;

  /// Four characters both devices show, so the person can see it is the
  /// right device before approving.
  String get check => publicKey
      .replaceAll(RegExp('[^A-Za-z0-9]'), '')
      .substring(0, 4)
      .toUpperCase();
}

/// One account's vault on this device.
class AccountVault extends ChangeNotifier {
  AccountVault({
    required this.session,
    required this.keys,
    required this.preferences,
    required this.profileTokens,
    required this.boundProfiles,
    this.onLoginsChanged,
  });
  final AccountSession session;
  final TokenStoreKeys keys;
  final SharedPreferences preferences;

  /// The token store of a profile on this device.
  final TokenStore Function(String profile) profileTokens;

  /// The profiles on this device that belong to this account.
  final List<String> Function() boundProfiles;

  /// Called after logins arrived for a profile, so it reloads them.
  final Future<void> Function(String profile)? onLoginsChanged;

  List<int>? _key;
  int keyVersion = 0;
  bool hasKey = false;
  List<WaitingDevice> waiting = const [];
  String? lastError;
  Future<void>? _running;
  bool _again = false;
  final _dirty = <String>{};
  bool closed = false;

  String get _prefix => 'account.${session.id}.vault';

  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  /// This device's own check characters.
  Future<String> ownCheck() async {
    final key = await keys.publicKey(session.id);
    return WaitingDevice('', '', key).check;
  }

  int _version(String profile) =>
      preferences.getInt('$_prefix.$profile.version') ?? 0;
  Json _times(String profile) {
    try {
      return (jsonDecode(
        preferences.getString('$_prefix.$profile.times') ?? '{}',
      ) as Map).cast<String, dynamic>();
    } catch (_) {
      return {};
    }
  }

  /// Logins this vault is writing itself, so they are not sent back.
  final _incoming = <String, String>{};

  /// Marks a profile's logins as changed here, to seal and send them.
  void changed(String profile, String key, [String? value]) {
    if (!vaultLogins.contains(key)) return;
    if (value != null && _incoming.remove('$profile/$key') == value) return;
    final times = _times(profile)
      ..[key] = DateTime.now().millisecondsSinceEpoch;
    unawaited(
      preferences.setString('$_prefix.$profile.times', jsonEncode(times)),
    );
    _dirty.add(profile);
    unawaited(sync());
  }

  /// Pulls the vault, makes or rotates the key when needed, takes logins
  /// that arrived and sends the ones changed here. One run at a time.
  Future<void> sync() {
    if (_running != null) {
      _again = true;
      return _running!;
    }
    return _running = () async {
      try {
        do {
          _again = false;
          await _sync();
        } while (_again && !closed);
        lastError = null;
      } catch (e) {
        lastError = e.toString();
      } finally {
        _running = null;
        notifyListeners();
      }
    }();
  }

  Future<void> _sync() async {
    if (closed || session.signedOut) return;
    final v = await session.request('GET', '/vault');
    keyVersion = number(v['key_version']);
    final mine = await keys.pair(session.id);
    final myPublic = b64url((await mine.extractPublicKey()).bytes);
    final devices = [
      for (final d in v['devices'] as List? ?? const [])
        if (d is Map && (d['public_key'] as String? ?? '').isNotEmpty)
          (d['id'] as String, d['public_key'] as String),
    ];
    if (keyVersion == 0) {
      // The first device makes the vault key and keeps the only copy.
      final fresh = List<int>.generate(32, (_) => Random.secure().nextInt(256));
      await session.request(
        'PUT',
        '/vault/keys',
        body: {
          'key_version': 1,
          'rotate': true,
          'check': await vaultCheck(fresh),
          'keys': {session.deviceId: await wrapKey(fresh, myPublic)},
        },
      );
      _key = fresh;
      keyVersion = 1;
      for (final p in boundProfiles()) {
        _dirty.add(p);
      }
    } else if (v['wrapped'] is String) {
      _key = await unwrapKey(v['wrapped'] as String, mine);
      if (v['has_check'] == false) {
        await session.request(
          'PUT',
          '/vault/check',
          body: {'key_version': keyVersion, 'check': await vaultCheck(_key!)},
        );
      }
    } else {
      _key = await _restore(myPublic);
    }
    hasKey = _key != null;
    if (_key case final key?) {
      final saved = await keys.savedVault(session.id);
      if (saved == null || saved.$1 != keyVersion) {
        await keys.saveVault(session.id, keyVersion, key);
      }
    }
    waiting = [
      for (final d in v['waiting'] as List? ?? const [])
        if (d is Map && d['id'] != session.deviceId)
          WaitingDevice(
            d['id'] as String,
            d['name'] as String? ?? 'Device',
            d['public_key'] as String,
          ),
    ];
    final key = _key;
    if (key == null) return;
    final blobs = {
      for (final b in v['blobs'] as List? ?? const [])
        if (b is Map) b['profile'] as String: b.cast<String, dynamic>(),
    };
    if (v['stale'] == true) {
      await _rotate(key, blobs, devices, v['holders'] as List? ?? const []);
      return;
    }
    final bound = boundProfiles().toSet();
    for (final MapEntry(key: profile, value: blob) in blobs.entries) {
      if (!bound.contains(profile)) continue;
      if (number(blob['version']) == _version(profile) ||
          number(blob['key_version']) != keyVersion) {
        continue;
      }
      await _take(
        profile,
        await open(key, blob['ciphertext'] as String),
        number(blob['version']),
      );
    }
    for (final profile in _dirty.toList()) {
      if (!bound.contains(profile)) {
        _dirty.remove(profile);
        continue;
      }
      await _send(profile, key, blobs[profile]);
      _dirty.remove(profile);
    }
  }

  /// Signed in again on a device that kept the vault key: proves it with
  /// the key's check and stores a copy for this device's new entry. After a
  /// device was removed the key was replaced, so an old one stays unused.
  Future<List<int>?> _restore(String myPublic) async {
    final saved = await keys.savedVault(session.id);
    if (saved == null || saved.$1 != keyVersion) return null;
    try {
      await session.request(
        'POST',
        '/vault/restore',
        body: {
          'key_version': keyVersion,
          'check': await vaultCheck(saved.$2),
          'wrapped': await wrapKey(saved.$2, myPublic),
        },
      );
    } on AccountFailure catch (e) {
      if (e.status == 403) await keys.forgetVault(session.id);
      return null;
    }
    return saved.$2;
  }

  /// Writes logins from the vault that are newer than this device's.
  Future<void> _take(String profile, Json logins, int version) async {
    final times = _times(profile);
    final store = profileTokens(profile);
    var changed = false;
    for (final key in vaultLogins) {
      final item = logins[key];
      if (item is! Map) continue;
      final value = item['v'], at = number(item['t']);
      // A sign-out stays on its own device: only logins travel.
      if (value is! String || value.isEmpty || at <= number(times[key])) {
        continue;
      }
      _incoming['$profile/$key'] = value;
      await store.write(key, value);
      times[key] = at;
      changed = true;
    }
    await preferences.setString('$_prefix.$profile.times', jsonEncode(times));
    await preferences.setInt('$_prefix.$profile.version', version);
    if (changed) await onLoginsChanged?.call(profile);
  }

  /// Seals this device's logins for [profile], merged with the server's.
  Future<void> _send(String profile, List<int> key, Json? current) async {
    var base = current;
    for (var attempt = 0; attempt < 3; attempt++) {
      final merged = <String, dynamic>{};
      if (base != null && number(base['key_version']) == keyVersion) {
        merged.addAll(await open(key, base['ciphertext'] as String));
      }
      final times = _times(profile);
      final store = profileTokens(profile);
      for (final k in vaultLogins) {
        final value = await store.read(k);
        final at = number(times[k]);
        final theirs = merged[k];
        if (value == null || value.isEmpty) continue;
        if (theirs is Map && number(theirs['t']) >= at && at > 0) continue;
        merged[k] = {'v': value, 't': at > 0 ? at : 1};
      }
      try {
        final r = await session.request(
          'PUT',
          '/vault/blob',
          body: {
            'profile': profile,
            'base_version': base == null ? 0 : number(base['version']),
            'key_version': keyVersion,
            'ciphertext': await seal(key, merged),
          },
        );
        await preferences.setInt(
          '$_prefix.$profile.version',
          number(r['version']),
        );
        return;
      } on AccountFailure catch (e) {
        if (e.status != 409) rethrow;
        final v = await session.request('GET', '/vault');
        base = [
          for (final b in v['blobs'] as List? ?? const [])
            if (b is Map && b['profile'] == profile) b.cast<String, dynamic>(),
        ].firstOrNull;
        if (base != null) {
          await _take(
            profile,
            await open(key, base['ciphertext'] as String),
            number(base['version']),
          );
        }
      }
    }
  }

  /// A device was removed: a new vault key, wrapped for the devices that
  /// still had the old one, and every blob sealed again.
  Future<void> _rotate(
    List<int> old,
    Map<String, Json> blobs,
    List<(String, String)> devices,
    List<dynamic> holders,
  ) async {
    final fresh = List<int>.generate(32, (_) => Random.secure().nextInt(256));
    final keep = holders.whereType<String>().toSet()..add(session.deviceId);
    final wrapped = <String, String>{
      for (final (id, pub) in devices)
        if (keep.contains(id)) id: await wrapKey(fresh, pub),
    };
    await session.request(
      'PUT',
      '/vault/keys',
      body: {
        'key_version': keyVersion + 1,
        'rotate': true,
        'check': await vaultCheck(fresh),
        'keys': wrapped,
      },
    );
    keyVersion++;
    _key = fresh;
    for (final MapEntry(key: profile, value: blob) in blobs.entries) {
      final logins = await open(old, blob['ciphertext'] as String);
      final r = await session.request(
        'PUT',
        '/vault/blob',
        body: {
          'profile': profile,
          'base_version': number(blob['version']),
          'key_version': keyVersion,
          'ciphertext': await seal(fresh, logins),
        },
      );
      if (boundProfiles().contains(profile)) {
        await preferences.setInt(
          '$_prefix.$profile.version',
          number(r['version']),
        );
      }
    }
  }

  /// Gives [device] the vault key, after the person checked its code.
  Future<void> approve(WaitingDevice device) async {
    final key = _key;
    if (key == null) {
      throw const AccountFailure('This device has no vault key yet.');
    }
    await session.request(
      'PUT',
      '/vault/keys',
      body: {
        'key_version': keyVersion,
        'keys': {device.id: await wrapKey(key, device.publicKey)},
      },
    );
    waiting = waiting.where((w) => w.id != device.id).toList();
    notifyListeners();
  }

  @override
  void dispose() {
    closed = true;
    super.dispose();
  }
}
