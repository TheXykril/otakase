import 'dart:async';

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
      splash: true,
    ),
  );
}

class OtakaseApp extends StatefulWidget {
  const OtakaseApp({
    super.key,
    required this.services,
    required this.isTV,
    this.splash = false,
  });
  final AppServices services;
  final bool isTV, splash;
  @override
  State<OtakaseApp> createState() => _OtakaseAppState();
}

class _OtakaseAppState extends State<OtakaseApp> {
  void modeChanged(FocusHighlightMode _) {
    if (mounted) setState(() {});
  }

  @override
  void initState() {
    super.initState();
    FocusManager.instance.addHighlightModeListener(modeChanged);
  }

  @override
  void dispose() {
    FocusManager.instance.removeHighlightModeListener(modeChanged);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final traditional =
        FocusManager.instance.highlightMode == FocusHighlightMode.traditional;
    final focusStyle = ButtonStyle(
      textStyle: WidgetStatePropertyAll(
        TextStyle(
          fontFamily: 'Inter',
          fontSize: widget.isTV ? 20 : 16,
          fontWeight: FontWeight.w600,
        ),
      ),
      foregroundColor: const WidgetStatePropertyAll(fg),
      overlayColor: WidgetStatePropertyAll(shu.withValues(alpha: .08)),
      shape: WidgetStatePropertyAll(
        RoundedRectangleBorder(borderRadius: BorderRadius.circular(999)),
      ),
      side: WidgetStateProperty.resolveWith(
        (s) => BorderSide(
          color: traditional && s.contains(WidgetState.focused)
              ? shu
              : Colors.transparent,
          width: 2,
        ),
      ),
    );
    return AppLayout(
      isTV: widget.isTV,
      child: MaterialApp(
        title: 'Otakase',
        debugShowCheckedModeBanner: false,
        theme: ThemeData(
          brightness: Brightness.dark,
          colorScheme: const ColorScheme.dark(
            primary: shu,
            onPrimary: fgBright,
            secondary: shu,
            surface: panel,
            outline: line,
            outlineVariant: line,
            onSurface: fg,
            error: warn,
          ),
          scaffoldBackgroundColor: bg,
          canvasColor: bg,
          fontFamily: 'Inter',
          fontFamilyFallback: const ['NotoSansJP'],
          textTheme: TextTheme(
            bodySmall: TextStyle(
              fontSize: widget.isTV ? 20 : 15,
              color: muted,
              height: 1.4,
            ),
            bodyMedium: TextStyle(
              fontSize: widget.isTV ? 20 : 16,
              color: fg,
              height: 1.4,
            ),
            bodyLarge: TextStyle(
              fontSize: widget.isTV ? 22 : 16,
              color: fg,
              height: 1.4,
            ),
            titleSmall: TextStyle(
              fontSize: widget.isTV ? 20 : 16,
              fontWeight: FontWeight.w600,
            ),
            titleMedium: TextStyle(
              fontSize: widget.isTV ? 22 : 18,
              fontWeight: FontWeight.w600,
            ),
            titleLarge: const TextStyle(
              fontSize: 26,
              fontWeight: FontWeight.w700,
            ),
            labelLarge: TextStyle(
              fontSize: widget.isTV ? 20 : 16,
              fontWeight: FontWeight.w600,
            ),
            labelMedium: TextStyle(fontSize: widget.isTV ? 20 : 15),
            labelSmall: TextStyle(fontSize: widget.isTV ? 20 : 15),
          ),
          focusColor: traditional ? line : Colors.transparent,
          dividerColor: line,
          dividerTheme: const DividerThemeData(color: line, thickness: 1),
          iconTheme: const IconThemeData(color: fg, size: 20),
          appBarTheme: AppBarTheme(
            backgroundColor: bg,
            surfaceTintColor: Colors.transparent,
            titleTextStyle: TextStyle(
              fontFamily: 'Inter',
              fontSize: widget.isTV ? 22 : 20,
              color: fg,
              fontWeight: FontWeight.w600,
            ),
          ),
          bottomSheetTheme: const BottomSheetThemeData(
            backgroundColor: panel,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.vertical(top: Radius.circular(20)),
            ),
          ),
          dialogTheme: DialogThemeData(
            backgroundColor: panel,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(20),
            ),
          ),
          textButtonTheme: TextButtonThemeData(style: focusStyle),
          outlinedButtonTheme: OutlinedButtonThemeData(style: focusStyle),
          iconButtonTheme: IconButtonThemeData(style: focusStyle),
          listTileTheme: const ListTileThemeData(
            textColor: fg,
            iconColor: muted,
          ),
          switchTheme: SwitchThemeData(
            thumbColor: WidgetStateProperty.resolveWith(
              (s) => s.contains(WidgetState.selected) ? fgBright : muted,
            ),
            trackColor: WidgetStateProperty.resolveWith(
              (s) => s.contains(WidgetState.selected) ? shu : line,
            ),
            trackOutlineColor: const WidgetStatePropertyAll(Colors.transparent),
          ),
          inputDecorationTheme: InputDecorationTheme(
            isDense: true,
            hintStyle: TextStyle(
              fontFamily: 'Inter',
              color: muted,
              fontSize: widget.isTV ? 20 : 16,
            ),
            labelStyle: TextStyle(
              fontFamily: 'Inter',
              color: muted,
              fontSize: widget.isTV ? 20 : 15,
            ),
            filled: true,
            fillColor: fgBright.withValues(alpha: .06),
            border: OutlineInputBorder(
              borderRadius: BorderRadius.circular(16),
              borderSide: BorderSide(color: line),
            ),
            enabledBorder: OutlineInputBorder(
              borderRadius: BorderRadius.circular(16),
              borderSide: BorderSide(color: line),
            ),
            focusedBorder: OutlineInputBorder(
              borderRadius: BorderRadius.circular(16),
              borderSide: BorderSide(color: shu),
            ),
            contentPadding: const EdgeInsets.symmetric(
              vertical: 16,
              horizontal: 16,
            ),
          ),
          sliderTheme: SliderThemeData(
            activeTrackColor: shu,
            inactiveTrackColor: line,
            thumbColor: fg,
            trackHeight: 2,
            thumbShape: const RoundSliderThumbShape(enabledThumbRadius: 4),
            overlayShape: const RoundSliderOverlayShape(overlayRadius: 14),
            overlayColor: shu.withValues(alpha: .1),
          ),
        ),
        home: widget.splash
            ? SplashGate(
                child: AppShell(services: widget.services, isTV: widget.isTV),
              )
            : AppShell(services: widget.services, isTV: widget.isTV),
      ),
    );
  }
}

class SplashGate extends StatefulWidget {
  const SplashGate({super.key, required this.child});
  final Widget child;
  @override
  State<SplashGate> createState() => _SplashGateState();
}

class _SplashGateState extends State<SplashGate> {
  Timer? timer;
  bool done = false, visible = false;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) setState(() => visible = true);
    });
    timer = Timer(const Duration(milliseconds: 1200), () {
      if (mounted) setState(() => done = true);
    });
  }

  @override
  void dispose() {
    timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedSwitcher(
    duration: const Duration(milliseconds: 300),
    child: done
        ? widget.child
        : Scaffold(
            key: const ValueKey('splash'),
            body: Center(
              child: AnimatedOpacity(
                opacity: visible ? 1 : 0,
                duration: const Duration(milliseconds: 300),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Image.asset('assets/mark.png', width: 150, height: 150),
                    const SizedBox(height: 32),
                    Text('お任せ', style: uiStyle(context, size: 20, color: fg)),
                    const SizedBox(height: 12),
                    Text(
                      'Leave it to us.',
                      style: uiStyle(context, color: muted),
                    ),
                  ],
                ),
              ),
            ),
          ),
  );
}
