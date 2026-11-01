/// True in Samsung TV (Tizen) builds. scripts/build-tpk.sh passes
/// --dart-define=OTAKASE_TIZEN=true; Android builds never set it.
const bool isTizen = bool.fromEnvironment('OTAKASE_TIZEN');

/// The TizenOS group this package was built for (6.0, 6.5 or 10.0).
const String tizenGroup = String.fromEnvironment('OTAKASE_TIZEN_API');

/// The installed update ("brain") the TV shell started, or null when the
/// code inside the package runs. Set from main's arguments.
String? tizenBrain;

/// Reads the shell's `--brain=<dir>` argument.
void readTizenArguments(List<String> args) {
  for (final arg in args) {
    if (arg.startsWith('--brain=')) tizenBrain = arg.substring(8);
  }
}
