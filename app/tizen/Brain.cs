using System;
using System.IO;
using Tizen;

namespace Runner
{
    /// <summary>
    /// Picks the Dart code to run. Updates live in data/brain/&lt;version&gt;/
    /// (flutter_assets, libapp.so, libotakasecore.so, libotakase_ass.so,
    /// shell-id). data/brain/active names the one to use. The app writes
    /// these after checking the update's signature; see lib/playback/tizen_updates.dart.
    /// </summary>
    internal static class Brain
    {
        public static string Choose(string data, string package)
        {
            try
            {
                var root = Path.Combine(data, "brain");
                var active = Path.Combine(root, "active");
                var booting = Path.Combine(root, "booting");
                if (!File.Exists(active))
                {
                    return null;
                }
                var name = File.ReadAllText(active).Trim();
                var dir = Path.Combine(root, name);
                // The app removes "booting" once it has started cleanly. Still
                // there means the last start with this update failed.
                if (File.Exists(booting) && File.ReadAllText(booting).Trim() == name)
                {
                    Log.Error("Otakase", $"Update {name} did not start; using the installed copy.");
                    File.WriteAllText(Path.Combine(root, "failed"), name);
                    File.Delete(active);
                    File.Delete(booting);
                    return null;
                }
                // An update only runs inside the shell it was built for: same
                // engine, plugins and TizenOS group.
                var shell = File.ReadAllText(Path.Combine(package, "shell-id")).Trim();
                var wanted = File.ReadAllText(Path.Combine(dir, "shell-id")).Trim();
                if (shell != wanted ||
                    !File.Exists(Path.Combine(dir, "libapp.so")) ||
                    !Directory.Exists(Path.Combine(dir, "flutter_assets")))
                {
                    File.Delete(active);
                    return null;
                }
                File.WriteAllText(booting, name);
                return dir;
            }
            catch (Exception e)
            {
                Log.Error("Otakase", $"Ignoring installed update: {e.Message}");
                return null;
            }
        }
    }
}
