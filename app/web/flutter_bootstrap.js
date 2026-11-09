{{flutter_js}}
{{flutter_build_config}}

// No service worker: the demo is part of the website and must update with it.
// Software rendering: with WebGL, pictures drawn before the player's video
// came up turn black once it goes away.
_flutter.loader.load({
  config: { canvasKitForceCpuOnly: true },
});
