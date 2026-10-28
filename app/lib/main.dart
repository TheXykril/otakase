import 'package:flutter/material.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'core.dart';
import 'data/catalog.dart';
import 'data/store.dart';
import 'services.dart';
import 'ui/app_shell.dart';
import 'ui/components.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  MediaKit.ensureInitialized();
  final core = CoreClient();
  final store = AppStore(await SharedPreferences.getInstance());
  runApp(
    OtakaseApp(
      services: AppServices(catalog: Catalog(), store: store, core: core),
      isTV: await core.isTV(),
    ),
  );
}

class OtakaseApp extends StatelessWidget {
  const OtakaseApp({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  Widget build(BuildContext context) {
    final focusStyle = ButtonStyle(
      side: WidgetStateProperty.resolveWith(
        (s) => BorderSide(
          color: s.contains(WidgetState.focused) ? shu : Colors.transparent,
          width: 2.5,
        ),
      ),
    );
    return MaterialApp(
      title: 'Otakase',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        brightness: Brightness.dark,
        colorScheme: ColorScheme.fromSeed(
          seedColor: shu,
          brightness: Brightness.dark,
          primary: shu,
          surface: panel,
        ),
        scaffoldBackgroundColor: Colors.black,
        canvasColor: Colors.black,
        fontFamily: 'NotoSansJP',
        focusColor: shu.withValues(alpha: .25),
        appBarTheme: const AppBarTheme(
          backgroundColor: Colors.black,
          surfaceTintColor: Colors.transparent,
        ),
        dialogTheme: const DialogThemeData(backgroundColor: panel),
        textButtonTheme: TextButtonThemeData(style: focusStyle),
        outlinedButtonTheme: OutlinedButtonThemeData(style: focusStyle),
        iconButtonTheme: IconButtonThemeData(style: focusStyle),
        listTileTheme: ListTileThemeData(
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(10),
          ),
        ),
        inputDecorationTheme: InputDecorationTheme(
          filled: true,
          fillColor: panel,
          border: OutlineInputBorder(
            borderRadius: BorderRadius.circular(16),
            borderSide: BorderSide.none,
          ),
          focusedBorder: OutlineInputBorder(
            borderRadius: BorderRadius.circular(16),
            borderSide: const BorderSide(color: shu, width: 2.5),
          ),
        ),
        sliderTheme: const SliderThemeData(
          activeTrackColor: shu,
          thumbColor: shu,
          overlayColor: Color(0x40d84836),
        ),
      ),
      home: AppShell(services: services, isTV: isTV),
    );
  }
}
