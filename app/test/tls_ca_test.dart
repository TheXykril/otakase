import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/tls_ca.dart';

const cert = '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----';

void main() {
  tearDown(resetTlsOptions);

  test('device CAs are joined into one file for mpv', () async {
    final tmp = await Directory.systemTemp.createTemp('ca');
    final system = await Directory('${tmp.path}/system').create();
    // Android stores each CA as PEM followed by a text dump.
    await File('${system.path}/a.0')
        .writeAsString('$cert\nCertificate:\n  Data');
    await File('${system.path}/b.0')
        .writeAsString(cert.replaceAll('MIIB', 'MIIC'));
    final options = await tlsOptions(
      dirs: [system.path, '${tmp.path}/missing'],
      cache: tmp,
    );
    final file = File(options['tls-ca-file']!);
    final text = await file.readAsString();
    expect('BEGIN CERTIFICATE'.allMatches(text), hasLength(2));
    expect(text, isNot(contains('Certificate:')));
    await tmp.delete(recursive: true);
  });

  test('no readable CAs leave mpv defaults alone', () async {
    final tmp = await Directory.systemTemp.createTemp('ca');
    expect(await tlsOptions(dirs: ['${tmp.path}/none'], cache: tmp), isEmpty);
    await tmp.delete(recursive: true);
  });
}
