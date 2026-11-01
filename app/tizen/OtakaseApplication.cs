// Based on FlutterApplication from flutter-tizen (BSD-style licence,
// Copyright 2020 Samsung Electronics Co., Ltd.). The only change is where
// the Dart code comes from: a verified update ("brain") in the app's data
// folder when one is installed, otherwise the copy inside the package.
using System;
using System.Collections.Generic;
using System.IO;
using System.Runtime.InteropServices;
using Tizen;
using Tizen.Applications;
using Tizen.Flutter.Embedding;

namespace Runner
{
    /// <summary>The engine with a public handle for the view API.</summary>
    internal class BrainEngine : FlutterEngine
    {
        public BrainEngine(string assets, string icu, string aot, List<string> args)
            : base(assets, icu, aot, "", args)
        {
        }

        public FlutterDesktopEngine Handle => Engine;
    }

    public class OtakaseApplication : CoreUIApplication, IPluginRegistry
    {
        private BrainEngine _engine;
        private FlutterDesktopView _view;

        public bool IsRunning => _view != null;

        public override void Run(string[] args)
        {
            AppDomain.CurrentDomain.UnhandledException += (s, e) =>
                Log.Error("Otakase", $"Unhandled exception: {e.ExceptionObject}");
#if TIZEN_CORE
            tizen_core_init();
            try
            {
                base.Run(args);
            }
            finally
            {
                tizen_core_shutdown();
            }
#else
            base.Run(args);
#endif
        }

        public FlutterDesktopPluginRegistrar GetRegistrarForPlugin(string pluginName) =>
            IsRunning ? _engine.GetRegistrarForPlugin(pluginName) : new FlutterDesktopPluginRegistrar();

        protected override void OnCreate()
        {
            base.OnCreate();
            var package = DirectoryInfo.Resource;
            var assets = Path.Combine(package, "flutter_assets");
            var icu = Path.Combine(package, "icudtl.dat");
            var aot = Path.Combine(package, "..", "lib", "libapp.so");
            var args = new List<string>();
            var brain = Brain.Choose(DirectoryInfo.Data, package);
            if (brain != null)
            {
                assets = Path.Combine(brain, "flutter_assets");
                aot = Path.Combine(brain, "libapp.so");
                args.Add("--brain=" + brain);
            }
            _engine = new BrainEngine(assets, icu, aot, args);
            if (!_engine.IsValid)
            {
                throw new Exception("Could not create a Flutter engine.");
            }
            var window = new WindowProperties
            {
                pointing_device_support = true,
                floating_menu_support = true,
            };
            _view = FlutterDesktopViewCreateFromNewWindow(ref window, _engine.Handle);
            if (_view.IsInvalid)
            {
                throw new Exception("Could not launch a Flutter application.");
            }
        }

        protected override void OnResume()
        {
            base.OnResume();
            _engine?.NotifyAppIsResumed();
        }

        protected override void OnPause()
        {
            base.OnPause();
            _engine?.NotifyAppIsPaused();
        }

        protected override void OnTerminate()
        {
            base.OnTerminate();
            _engine?.NotifyAppIsDetached();
            DotnetPluginRegistry.Instance.RemoveAllPlugins();
            if (_view != null)
            {
                FlutterDesktopViewDestroy(_view);
            }
            _engine = null;
            _view = null;
        }

        protected override void OnAppControlReceived(AppControlReceivedEventArgs e) =>
            _engine?.NotifyAppControl(e.ReceivedAppControl);

        protected override void OnLowMemory(LowMemoryEventArgs e)
        {
            base.OnLowMemory(e);
            _engine?.NotifyLowMemoryWarning();
        }

        protected override void OnLocaleChanged(LocaleChangedEventArgs e)
        {
            base.OnLocaleChanged(e);
            _engine?.NotifyLocaleChange();
        }

        protected override void OnRegionFormatChanged(RegionFormatChangedEventArgs e)
        {
            base.OnRegionFormatChanged(e);
            _engine?.NotifyLocaleChange();
        }

        // Must match FlutterDesktopWindowProperties in flutter_tizen.h.
        [StructLayout(LayoutKind.Sequential)]
        private struct WindowProperties
        {
            public int x;
            public int y;
            public int width;
            public int height;
            [MarshalAs(UnmanagedType.U1)]
            public bool transparent;
            [MarshalAs(UnmanagedType.U1)]
            public bool focusable;
            [MarshalAs(UnmanagedType.U1)]
            public bool top_level;
            public int renderer_type;
            public double user_pixel_ratio;
            public IntPtr window_handle;
            [MarshalAs(UnmanagedType.U1)]
            public bool pointing_device_support;
            [MarshalAs(UnmanagedType.U1)]
            public bool floating_menu_support;
        }

        [DllImport("flutter_tizen.so")]
        private static extern FlutterDesktopView FlutterDesktopViewCreateFromNewWindow(
            ref WindowProperties properties, FlutterDesktopEngine engine);

        [DllImport("flutter_tizen.so")]
        private static extern void FlutterDesktopViewDestroy(FlutterDesktopView view);

#if TIZEN_CORE
        [DllImport("libtizen-core.so.0")]
        private static extern void tizen_core_init();

        [DllImport("libtizen-core.so.0")]
        private static extern void tizen_core_shutdown();
#endif
    }
}
