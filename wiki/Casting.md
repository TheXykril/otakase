# Casting

The cast button in the player sends the episode to a Chromecast, a DLNA TV
or Kodi on the same network. Most smart TVs (LG, Samsung, Sony, Hisense,
Philips) are DLNA renderers and show up in the list on their own. The last
entry, **Any phone, TV or console on this network**, shows a link and a QR
code: open it in any browser on the network and the episode plays there.

While casting, the player controls drive the TV. **Stop casting** ends it.

## No TVs found

- The TV must be on (not in deep standby) and on the same network as this
  device, not a guest Wi-Fi.
- On a DLNA TV, check that its "media renderer" or "DLNA" setting is on.
- **On a computer, a firewall is the usual cause.** TVs answer the search
  over UDP, which ufw and firewalld drop by default. A quick test on Linux:
  `sudo ufw disable`, search again, `sudo ufw enable`. If the TV shows up
  with the firewall off, allow your home network in, for example:

  ```bash
  sudo ufw allow from 192.168.0.0/24 comment 'otakase cast'
  ```

  Replace `192.168.0.0/24` with your own network. On Windows, allow Otakase
  on private networks when Windows asks.

## The TV shows nothing, or stops partway

The TV fetches the video from this device, so the same firewall can block
it after the TV was found. Allow your home network as above and cast again.
If it still fails, open **Diagnostics** (in More or Settings) and send what
it shows in the support forum on
[Discord](https://otakase.xyverion.com/discord/).

## No subtitles on the TV

Some TVs ignore separate subtitle tracks. On a computer with
[ffmpeg](https://ffmpeg.org) installed, Otakase draws the subtitles into the
picture instead, on its own.
