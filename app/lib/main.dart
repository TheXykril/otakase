import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';

import 'core.dart';
import 'player.dart';

const shu = Color(0xffd84836);

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  MediaKit.ensureInitialized();
  final core = CoreClient();
  runApp(SpikeApp(core: core, isTV: await core.isTV()));
}

class SpikeApp extends StatelessWidget {
  const SpikeApp({super.key, required this.core, required this.isTV});
  final CoreClient core;
  final bool isTV;
  @override
  Widget build(BuildContext context) {
    final focusStyle = ButtonStyle(
      side: WidgetStateProperty.resolveWith(
        (states) => BorderSide(
          color: states.contains(WidgetState.focused) ? shu : Colors.grey,
          width: states.contains(WidgetState.focused) ? 3 : 1,
        ),
      ),
      minimumSize: const WidgetStatePropertyAll(Size(56, 52)),
    );
    return MaterialApp(
      title: 'Otakase',
      theme: ThemeData(
        brightness: Brightness.dark,
        colorSchemeSeed: shu,
        scaffoldBackgroundColor: const Color(0xff111116),
        outlinedButtonTheme: OutlinedButtonThemeData(style: focusStyle),
        textButtonTheme: TextButtonThemeData(style: focusStyle),
        inputDecorationTheme: const InputDecorationTheme(
          border: OutlineInputBorder(),
          focusedBorder: OutlineInputBorder(
            borderSide: BorderSide(color: shu, width: 3),
          ),
        ),
      ),
      home: SearchPage(core: core, isTV: isTV),
    );
  }
}

class SearchPage extends StatefulWidget {
  const SearchPage({super.key, required this.core, required this.isTV});
  final CoreClient core;
  final bool isTV;
  @override
  State<SearchPage> createState() => _SearchPageState();
}

class _SearchPageState extends State<SearchPage> {
  final query = TextEditingController(text: 'Frieren');
  String provider = 'megavid';
  String mode = 'sub';
  String? error;
  List<Map<String, dynamic>> shows = [];
  bool busy = false;
  int? handle;
  int generation = 0;

  void cancel() {
    generation++;
    if (handle case final int id) unawaited(widget.core.cancel(id));
    handle = null;
    if (mounted) setState(() => busy = false);
  }

  Future<void> search() async {
    cancel();
    final current = generation;
    setState(() {
      busy = true;
      error = null;
      shows = [];
    });
    try {
      final data = await widget.core.request(
        {
          'operation': 'search',
          'provider': provider,
          'mode': mode,
          'query': query.text,
        },
        onHandle: (id) {
          if (!mounted || current != generation) {
            unawaited(widget.core.cancel(id));
          } else {
            handle = id;
          }
        },
      );
      if (mounted && current == generation) {
        setState(
          () => shows = (data['shows'] as List).cast<Map<String, dynamic>>(),
        );
      }
    } catch (e) {
      if (mounted && current == generation) {
        setState(() => error = safeError(e));
      }
    } finally {
      if (mounted && current == generation) {
        setState(() {
          busy = false;
          handle = null;
        });
      }
    }
  }

  Future<void> editQuery() async {
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('Search anime'),
        content: SizedBox(
          width: 480,
          child: TextField(
            controller: query,
            autofocus: true,
            textInputAction: TextInputAction.search,
            onSubmitted: (_) {
              Navigator.pop(dialogContext);
              unawaited(search());
            },
          ),
        ),
        actions: [
          OutlinedButton(
            onPressed: () => Navigator.pop(dialogContext),
            child: const Text('Back'),
          ),
          OutlinedButton(
            onPressed: () {
              Navigator.pop(dialogContext);
              unawaited(search());
            },
            child: const Text('Search'),
          ),
        ],
      ),
    );
    if (mounted) setState(() {});
  }

  Future<void> back() async {
    if (busy) {
      cancel();
      return;
    }
    final exit = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Exit Otakase?'),
        actions: [
          OutlinedButton(
            autofocus: true,
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Stay'),
          ),
          OutlinedButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Exit'),
          ),
        ],
      ),
    );
    if (exit == true) await SystemNavigator.pop();
  }

  @override
  void dispose() {
    generation++;
    if (handle case final int id) unawaited(widget.core.cancel(id));
    query.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => PopScope(
    canPop: false,
    onPopInvokedWithResult: (didPop, result) {
      if (!didPop) unawaited(back());
    },
    child: Scaffold(
      appBar: AppBar(
        title: Text('Otakase · ${widget.isTV ? 'TV' : 'Phone'} spike'),
      ),
      body: SafeArea(
        child: FocusTraversalGroup(
          child: ListView(
            padding: EdgeInsets.all(widget.isTV ? 32 : 16),
            children: [
              const Text(
                'Provider playback probe',
                style: TextStyle(fontSize: 26),
              ),
              const SizedBox(height: 16),
              Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  for (final value in ['megavid', 'anikoto'])
                    OutlinedButton(
                      autofocus: value == 'megavid',
                      onPressed: () {
                        cancel();
                        setState(() {
                          provider = value;
                          shows = [];
                        });
                      },
                      child: Text('${provider == value ? '✓ ' : ''}$value'),
                    ),
                  for (final value in ['sub', 'dub'])
                    OutlinedButton(
                      onPressed: () {
                        cancel();
                        setState(() {
                          mode = value;
                          shows = [];
                        });
                      },
                      child: Text('${mode == value ? '✓ ' : ''}$value'),
                    ),
                ],
              ),
              const SizedBox(height: 16),
              if (widget.isTV)
                OutlinedButton(
                  onPressed: editQuery,
                  child: Text('Search: ${query.text}'),
                )
              else
                TextField(
                  controller: query,
                  decoration: const InputDecoration(labelText: 'Search anime'),
                  textInputAction: TextInputAction.search,
                  onSubmitted: (_) => search(),
                ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  OutlinedButton(
                    key: const Key('search-button'),
                    onPressed: search,
                    child: const Text('Search'),
                  ),
                  if (busy)
                    OutlinedButton(
                      onPressed: cancel,
                      child: const Text('Cancel'),
                    ),
                  OutlinedButton(
                    onPressed: () => Navigator.push(
                      context,
                      MaterialPageRoute<void>(
                        builder: (_) => const PlaybackPage(probe: true),
                      ),
                    ),
                    child: const Text('HLS headers + ASS probe'),
                  ),
                ],
              ),
              if (busy)
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: LinearProgressIndicator(),
                ),
              if (error != null)
                Padding(padding: const EdgeInsets.all(16), child: Text(error!)),
              for (final show in shows)
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: OutlinedButton(
                    key: ValueKey('show-${show['id']}'),
                    onPressed: () => Navigator.push(
                      context,
                      MaterialPageRoute<void>(
                        builder: (_) => EpisodesPage(
                          core: widget.core,
                          provider: provider,
                          mode: mode,
                          show: show,
                        ),
                      ),
                    ),
                    child: Padding(
                      padding: const EdgeInsets.all(12),
                      child: Text(show['label'] as String),
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    ),
  );
}

String safeError(Object error) => error is CoreFailure
    ? error.toString()
    : 'Request failed. Retry or choose another provider.';

class EpisodesPage extends StatefulWidget {
  const EpisodesPage({
    super.key,
    required this.core,
    required this.provider,
    required this.mode,
    required this.show,
  });
  final CoreClient core;
  final String provider;
  final String mode;
  final Map<String, dynamic> show;
  @override
  State<EpisodesPage> createState() => _EpisodesPageState();
}

class _EpisodesPageState extends State<EpisodesPage> {
  List<String> episodes = [];
  String? error;
  bool busy = true;
  int? handle;
  int generation = 0;
  @override
  void initState() {
    super.initState();
    unawaited(load());
  }

  Future<void> load({String? episode}) async {
    cancel();
    final current = generation;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      final data = await widget.core.request(
        {
          'operation': episode == null ? 'episodes' : 'resolve',
          'provider': widget.provider,
          'mode': widget.mode,
          'show_id': widget.show['id'],
          if (episode != null) 'episode': int.parse(episode),
        },
        onHandle: (id) {
          if (!mounted || current != generation) {
            unawaited(widget.core.cancel(id));
          } else {
            handle = id;
          }
        },
      );
      if (!mounted || current != generation) return;
      if (episode == null) {
        setState(() => episodes = (data['episodes'] as List).cast<String>());
      } else {
        final streams = (data['streams'] as List).cast<Map<String, dynamic>>();
        await Navigator.push(
          context,
          MaterialPageRoute<void>(
            builder: (_) => PlaybackPage(
              streams: streams.map(ResolvedStream.fromJson).toList(),
            ),
          ),
        );
      }
    } catch (e) {
      if (mounted && current == generation) {
        setState(() => error = safeError(e));
      }
    } finally {
      if (mounted && current == generation) {
        setState(() {
          busy = false;
          handle = null;
        });
      }
    }
  }

  void cancel() {
    generation++;
    if (handle case final int id) unawaited(widget.core.cancel(id));
    handle = null;
    if (mounted) setState(() => busy = false);
  }

  @override
  void dispose() {
    generation++;
    if (handle case final int id) unawaited(widget.core.cancel(id));
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(widget.show['title'] as String)),
    body: SafeArea(
      child: ListView(
        padding: const EdgeInsets.all(24),
        children: [
          Text('${widget.provider} · ${widget.mode}'),
          if (busy) const LinearProgressIndicator(),
          if (busy)
            OutlinedButton(
              autofocus: true,
              onPressed: cancel,
              child: const Text('Cancel'),
            ),
          if (error != null) Text(error!),
          if (episodes.isEmpty && !busy)
            OutlinedButton(onPressed: load, child: const Text('Retry')),
          for (final episode in episodes)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: OutlinedButton(
                autofocus: episode == episodes.first,
                onPressed: busy ? null : () => load(episode: episode),
                child: Padding(
                  padding: const EdgeInsets.all(12),
                  child: Text('Play episode $episode'),
                ),
              ),
            ),
        ],
      ),
    ),
  );
}
