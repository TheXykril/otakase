/// True in Samsung TV (Tizen) builds. scripts/build-tpk.sh passes
/// --dart-define=OTAKASE_TIZEN=true; Android builds never set it.
const bool isTizen = bool.fromEnvironment('OTAKASE_TIZEN');
