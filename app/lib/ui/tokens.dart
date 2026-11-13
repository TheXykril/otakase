// Design tokens: the only place colours, corner radii, spacing and motion
// values are written down. Screens and components refer to these names.
import 'package:flutter/animation.dart';

// Colour.
const bg = Color(0xff0e0d0c),
    bgDeep = Color(0xff080707),
    panel = Color(0xff191614),
    line = Color(0xff3c332d),
    fg = Color(0xffece4d6),
    fgBright = Color(0xfffff8ec),
    muted = Color(0xffb9b0a4),
    panelRaised = Color(0xff211b18),
    selectedPanel = Color(0xff2b221e),
    lineStrong = Color(0xff655046),
    accentText = Color(0xfff28b74),
    disabled = Color(0xff8a8276),
    shu = Color(0xffd2492f),
    accentDeep = Color(0xffa83a22),
    ok = Color(0xff9bb67a),
    warn = Color(0xffe3b45a),
    info = Color(0xff7fa2c4);

/// Destructive actions and errors. Cautions keep [warn].
const danger = KageColors.danger;

/// Divider between rows, quieter than [line].
const hairline = Color(0xff2a2420);

/// Soft drop shadow under floating plates.
const scrim = Color(0x99000000);

/// Seek bars: the part not played yet, the part already buffered, and
/// the striped band under a skip span.
const trackRest = Color(0xff5a5148),
    trackBuffered = Color(0xffa09282),
    skipBand = Color(0xff302923);

/// Edge of the phone resume card.
const resumeEdge = Color(0xff4a342a);

/// The seal mark's shaded side.
const sealShade = Color(0xff582419);

/// The desktop header island, a step darker than [bgDeep].
const islandFill = Color(0xff0b0a09);

/// White behind QR codes, which scanners need.
const qrPaper = Color(0xffffffff);

/// Black shades over video and art, by strength.
const shadeClear = Color(0x00000000),
    shade40 = Color(0x66000000),
    shade70 = Color(0xb3000000),
    shade80 = Color(0xcc000000);

/// The page background fading into art: [bg] at a given opacity.
const bgClear = Color(0x000e0d0c),
    bg20 = Color(0x330e0d0c),
    bg25 = Color(0x400e0d0c),
    bg30 = Color(0x500e0d0c),
    bg60 = Color(0x990e0d0c),
    bg85 = Color(0xd90e0d0c),
    bg90 = Color(0xe60e0d0c);

/// [bgDeep] and [panel] at a given opacity, for plates over video.
const bgDeepClear = Color(0x00080707),
    bgDeep80 = Color(0xcc080707),
    bgDeep90 = Color(0xe6080707),
    panel90 = Color(0xe6191614);

/// Window buttons drawn the way each desktop draws its own.
abstract final class WindowColors {
  static const hover = Color(0x1affffff),
      pressed = Color(0x33ffffff),
      windowsClose = Color(0xffc42b1c),
      kdeClose = Color(0xffda4453),
      kdeIcon = Color(0xffeff0f1);
}

/// Corner radii: four steps and a circle, then the role each part of the
/// app uses. Parts pick a role, never a number.
abstract final class KageRadius {
  static const xs = 4.0, sm = 8.0, md = 12.0, lg = 16.0;

  /// Circle or capsule: avatars, player transport, switch, slider thumb,
  /// count badges, pager dots.
  static const full = 999.0;

  // Controls.
  static const button = sm;
  static const tab = sm;
  static const chip = sm;
  static const toast = sm;
  static const input = sm;

  /// Segment inside a segmented plate: its corner less the 2 px padding.
  static const segment = input - 2;

  // Artwork. Never rounder than md.
  static const poster = md;
  static const thumb = sm;
  static const tag = xs;
  static const spine = xs;

  // Surfaces.
  static const tile = md;
  static const panel = lg;
  static const menu = md;
  static const navBar = lg;

  /// Tile inside the nav bar: the bar's corner less its 4 px padding,
  /// which leaves each tab a 48 px touch target.
  static const navTile = navBar - 4;
  static const dialog = lg;
  static const sheet = lg;

  /// Thin bars: tab underlines, seek rails, battery outline.
  static const mark = 2.0, track = 3.0;
}

/// Spacing scale, 4-based.
abstract final class KageSpace {
  static const s1 = 4.0,
      s2 = 8.0,
      s3 = 12.0,
      s4 = 16.0,
      s5 = 24.0,
      s6 = 32.0,
      s7 = 48.0,
      s8 = 64.0;
}

/// Durations and curves.
abstract final class KageMotion {
  static const fast = Duration(milliseconds: 100),
      base = Duration(milliseconds: 200),
      slow = Duration(milliseconds: 320);
  static const enter = Curves.easeOutCubic, exit = Curves.easeInCubic;

  /// Moving something already on screen, such as scrolling it into view.
  static const move = Curves.ease;

  /// Slow crossfades between big pictures, such as hero banners.
  static const hero = Duration(milliseconds: 600);

  /// One sweep of a loading skeleton.
  static const shimmer = Duration(milliseconds: 1500);
}

/// Colour names that local variables tend to shadow (a `danger` flag).
abstract final class KageColors {
  static const danger = Color(0xffe5604a);
}

/// The screen a text size is chosen for. Desktop sizes are in the scaled
/// 1920 × 1080 window canvas, TV sizes in the 1600 × 900 TV canvas; the
/// desktop launcher is not scaled and uses [KageType.launcher].
enum TypeScreen { phone, desktop, tv }

/// Named text sizes with one column per screen. Every size respects the
/// screen's legibility floor, which is checked by a test rather than
/// silently raised at runtime.
enum TextRole { display, title, heading, body, label, meta, kicker }

abstract final class KageType {
  /// The smallest text each screen shows. Desktop sits close to the viewer,
  /// so it reads smaller text than a TV across the room.
  static const floor = {
    TypeScreen.phone: 15.0,
    TypeScreen.desktop: 17.0,
    TypeScreen.tv: 20.0,
  };

  /// The unscaled desktop launcher window.
  static const launcher = 13.0;

  /// Mono kickers ("EP 03 · 17 MIN LEFT") may sit below the phone floor:
  /// a few uppercase characters stay readable at 12.
  static const phoneKicker = 12.0;

  static const sizes = {
    TextRole.display: (phone: 30.0, desktop: 56.0, tv: 48.0),
    TextRole.title: (phone: 22.0, desktop: 32.0, tv: 32.0),
    TextRole.heading: (phone: 18.0, desktop: 24.0, tv: 26.0),
    TextRole.body: (phone: 15.0, desktop: 18.0, tv: 22.0),
    TextRole.label: (phone: 15.0, desktop: 17.0, tv: 22.0),
    TextRole.meta: (phone: 15.0, desktop: 17.0, tv: 20.0),
    TextRole.kicker: (phone: 12.0, desktop: 17.0, tv: 20.0),
  };

  static double size(TextRole role, TypeScreen screen) {
    final s = sizes[role]!;
    return switch (screen) {
      TypeScreen.phone => s.phone,
      TypeScreen.desktop => s.desktop,
      TypeScreen.tv => s.tv,
    };
  }
}
