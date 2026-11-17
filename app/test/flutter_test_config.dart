import 'dart:async';
import 'dart:io';

import 'test_environment.dart';

/// Runs before every test file: the developer's desktop session and time
/// zone must not change what the tests see.
Future<void> testExecutable(FutureOr<void> Function() testMain) async {
  pinTestHost(Platform.environment);
  await testMain();
}
