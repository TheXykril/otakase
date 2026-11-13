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
}
