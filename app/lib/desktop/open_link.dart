import 'dart:io';

/// Starts a program. Tests replace it.
typedef LinkOpener = Future<ProcessResult> Function(
  String executable,
  List<String> arguments,
);

LinkOpener debugLinkOpener = Process.run;

/// Opens [uri] in the default browser of the computer. Only http and https
/// links are passed on, so nothing else can be started this way.
Future<bool> openInBrowser(Uri uri) async {
  if (uri.scheme != 'https' && uri.scheme != 'http') return false;
  final link = uri.toString();
  final (String executable, List<String> arguments) = Platform.isWindows
      ? ('rundll32', ['url.dll,FileProtocolHandler', link])
      : Platform.isMacOS
      ? ('open', [link])
      : ('xdg-open', [link]);
  try {
    final result = await debugLinkOpener(executable, arguments);
    return result.exitCode == 0;
  } on ProcessException {
    return false;
  }
}
