# Casting Problems

## No devices found / the TV isn't in the list

**Usually a firewall on the computer running otakase.** Discovery asks the
network who can play video, and the answers come back as inbound UDP, which
ufw and firewalld drop by default. The TV is there; its answer just never
arrives. A quick test: `sudo ufw disable`, search again, `sudo ufw enable`.
If the TV shows up with the firewall off, the easiest fix is one command:

```bash
otakase -cast-setup
```

It fixes `CastPort` and `CastDiscoveryPort` in your config if they are random,
shows the three rules below for your own network, runs them with `sudo` after
you say yes (sudo asks for your password), then searches and lists the
devices it finds. The rules only allow your LAN and are tagged
`otakase cast` in `sudo ufw status`, so they're easy to find and delete.

Or allow the answers in by hand:

```bash
sudo ufw allow from 192.168.0.0/24 to any port 8011 proto udp   # DLNA TV answers (CastDiscoveryPort)
sudo ufw allow from 192.168.0.0/24 to any port 5353 proto udp   # mDNS (Chromecast, Kodi)
sudo ufw allow from 192.168.0.0/24 to any port 8010 proto tcp   # the stream (CastPort)
```

Replace `192.168.0.0/24` with your own network, `8011` with your
`CastDiscoveryPort` and `8010` with your `CastPort`. When a search finds
nothing and ufw or firewalld is on, otakase puts these rules for your own
subnet and ports, plus a pointer to `-cast-setup`, in the "no devices found" message (in the terminal, or a
desktop notification when started from rofi), and copies them to your
clipboard: paste into a terminal and run.

`CastDiscoveryPort` (default `8011`) arrived in 26.2.0.
On older releases, TVs answer on a random port, so the only rule that
works is opening all UDP from your LAN:
`sudo ufw allow from 192.168.0.0/24 proto udp`.

If the firewall isn't the cause: check the TV is on (not in deep standby),
on the same network (not a guest Wi-Fi), and that its "media renderer" or
"DLNA" setting is on. For Kodi, set `KodiHost` to its address.

## The episode never starts / the TV never plays anything

**Almost always a firewall on the computer running otakase**, blocking the
Chromecast from reaching it. otakase detects this itself: when a cast fails
because the device never fetched anything, it prints the exact fix and
**copies it to your clipboard**. Open a terminal, paste, run it, cast again.

If that doesn't fix it, or otakase couldn't detect your firewall:

```bash
# Make sure CastPort is a fixed value in your config first (otakase -e),
# then allow that one port from your LAN:
sudo ufw allow from 192.168.0.0/24 to any port 8010 proto tcp comment 'otakase cast'
```

Replace `192.168.0.0/24` with your own network and `8010` with your `CastPort`.

### Still not working after that?

Two things otakase can't check for you, because checking needs root:

- **A second firewall service is overriding the one you just fixed.**
  `ufw status` reads its *saved config*, not the live kernel rules — so it can
  say a rule is allowed while the connection is still blocked. The usual
  culprit is `iptables.service` (or `nftables.service`) reapplying its own
  rules at every boot, underneath ufw. Check:
  ```bash
  systemctl is-enabled iptables ip6tables nftables
  ```
  If any of those are enabled on a ufw system, disable them — ufw manages its
  own rules and doesn't need them:
  ```bash
  sudo systemctl disable --now iptables ip6tables
  ```

- **Docker is intercepting the port.** If Docker is installed and running, it
  rewrites firewall rules on its own, ahead of ufw's chain, and does it again
  every time it restarts. Check:
  ```bash
  sudo iptables -L DOCKER-USER -n -v
  ```

To confirm the rule is actually live (not just saved), check the kernel table
directly rather than trusting `ufw status`:

```bash
sudo iptables -L ufw-user-input -n -v | grep 8010
```

An empty result means the rule exists in ufw's config but never made it into
the kernel — re-apply with `sudo ufw allow ...` (not by editing the rules file
directly), since `ufw allow` writes the config *and* loads it.

## No subtitles appear

- If the episode has subtitles and you expected them burned into the picture,
  check `CastBurnSubtitles` is `true` in your config (`otakase -e`).
- If burning is on and subtitles still don't show, the provider likely
  supplied no subtitle track for that episode/mode combination — check
  `otakase-debug.log` for a line saying why burning was skipped.
- A Chromecast can only ever show subtitles that are burned into the video.
  It has no support for reading a separate subtitle track from the stream.

## Wrong device, or asked every time

Set `CastDevice` to the device's name in your config to skip being asked which
device to use each time.

## The wrong terminal opens (or none does) when casting from rofi

Set `CastTerminal` in your config to the terminal emulator you want used.

## Seeking does nothing, or is slow

Chromecast can't seek a stream mid-episode on its own — otakase works around
this by rebuilding the stream at the position you asked for and handing the
device a fresh one, which takes a couple of seconds. That's why a single
press moves 30 seconds instead of 10, and why holding the key waits until you
stop pressing before it jumps — each seek costs a few seconds of rebuffering,
so it batches your presses instead of restarting the stream on every one.

## Casting fails partway through, or subtitles seem to drift out of sync after a seek

Check `otakase-debug.log` — every cast-related failure is logged with the
exact reason. If ffmpeg is involved (burning subtitles, or a slow encode),
its own error output is captured there too.

---

If none of this covers it, [open an issue](https://github.com/TheXykril/otakase/issues)
with the relevant lines from `otakase-debug.log`.
