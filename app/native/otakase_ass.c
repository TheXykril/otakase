// ASS subtitle renderer for the Samsung TV app: libass draws a subtitle
// frame and this file blends its bitmaps into one premultiplied RGBA image
// covering only the area with subtitles. Loaded by lib/playback/ass_overlay.dart
// through dart:ffi.

#include <ass/ass.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
  ASS_Library *library;
  ASS_Renderer *renderer;
  ASS_Track *track;
  int width, height;
  uint8_t *pixels;
  size_t capacity;
} otakase_ass;

static void quiet(int level, const char *format, va_list args, void *data) {
  (void)level, (void)format, (void)args, (void)data;
}

otakase_ass *otakase_ass_new(void) {
  otakase_ass *a = calloc(1, sizeof(*a));
  if (!a) return NULL;
  a->library = ass_library_init();
  if (!a->library) {
    free(a);
    return NULL;
  }
  ass_set_message_cb(a->library, quiet, NULL);
  // Fonts embedded in the subtitle file.
  ass_set_extract_fonts(a->library, 1);
  a->renderer = ass_renderer_init(a->library);
  if (!a->renderer) {
    ass_library_done(a->library);
    free(a);
    return NULL;
  }
  return a;
}

// Adds a font file held in memory; libass keeps its own copy.
void otakase_ass_add_font(otakase_ass *a, const char *name, const char *data,
                          int size) {
  ass_add_font(a->library, name, data, size);
}

// Call after the fonts are added. Missing families fall back to
// default_family, so it must name one of the added fonts.
void otakase_ass_set_default_font(otakase_ass *a, const char *default_family) {
  ass_set_fonts(a->renderer, NULL, default_family, ASS_FONTPROVIDER_NONE, NULL,
                0);
}

// Replaces the subtitle file. Returns 0 on success.
int otakase_ass_load(otakase_ass *a, char *data, int size) {
  if (a->track) ass_free_track(a->track);
  a->track = ass_read_memory(a->library, data, size, NULL);
  return a->track ? 0 : -1;
}

// Size in physical pixels of the video rectangle the subtitles cover.
void otakase_ass_set_size(otakase_ass *a, int width, int height) {
  if (width == a->width && height == a->height) return;
  a->width = width;
  a->height = height;
  ass_set_frame_size(a->renderer, width, height);
  ass_set_storage_size(a->renderer, width, height);
}

static inline unsigned mul255(unsigned v) { return (v + 127) / 255; }
static inline uint8_t over(unsigned top, unsigned below) {
  unsigned v = top + below;
  return v > 255 ? 255 : (uint8_t)v;
}

// Renders the subtitles at time_ms. Returns 0 when nothing changed since the
// previous call, 1 with a new image in *pixels (premultiplied RGBA, rect
// x/y/w/h inside the video), 2 when nothing is shown, -1 on error.
int otakase_ass_render(otakase_ass *a, long long time_ms, int *x, int *y,
                       int *w, int *h, uint8_t **pixels) {
  if (!a->track || a->width <= 0 || a->height <= 0) return -1;
  int change = 0;
  ASS_Image *images =
      ass_render_frame(a->renderer, a->track, time_ms, &change);
  if (!change) return 0;
  if (!images) return 2;

  int left = a->width, top = a->height, right = 0, bottom = 0;
  for (ASS_Image *i = images; i; i = i->next) {
    if (i->w <= 0 || i->h <= 0) continue;
    if (i->dst_x < left) left = i->dst_x;
    if (i->dst_y < top) top = i->dst_y;
    if (i->dst_x + i->w > right) right = i->dst_x + i->w;
    if (i->dst_y + i->h > bottom) bottom = i->dst_y + i->h;
  }
  if (left < 0) left = 0;
  if (top < 0) top = 0;
  if (right > a->width) right = a->width;
  if (bottom > a->height) bottom = a->height;
  if (right <= left || bottom <= top) return 2;

  int bw = right - left, bh = bottom - top;
  size_t need = (size_t)bw * bh * 4;
  if (need > a->capacity) {
    uint8_t *grown = realloc(a->pixels, need);
    if (!grown) return -1;
    a->pixels = grown;
    a->capacity = need;
  }
  memset(a->pixels, 0, need);

  for (ASS_Image *i = images; i; i = i->next) {
    unsigned r = i->color >> 24, g = (i->color >> 16) & 0xff,
             b = (i->color >> 8) & 0xff, opacity = 255 - (i->color & 0xff);
    for (int row = 0; row < i->h; row++) {
      int py = i->dst_y + row - top;
      if (py < 0 || py >= bh) continue;
      const uint8_t *src = i->bitmap + (size_t)row * i->stride;
      uint8_t *dst = a->pixels + ((size_t)py * bw) * 4;
      for (int col = 0; col < i->w; col++) {
        int px = i->dst_x + col - left;
        if (px < 0 || px >= bw || !src[col]) continue;
        unsigned alpha = mul255(src[col] * opacity);
        if (!alpha) continue;
        uint8_t *p = dst + px * 4;
        unsigned keep = 255 - alpha;
        p[0] = over(mul255(r * alpha), mul255(p[0] * keep));
        p[1] = over(mul255(g * alpha), mul255(p[1] * keep));
        p[2] = over(mul255(b * alpha), mul255(p[2] * keep));
        p[3] = over(alpha, mul255(p[3] * keep));
      }
    }
  }
  *x = left;
  *y = top;
  *w = bw;
  *h = bh;
  *pixels = a->pixels;
  return 1;
}

void otakase_ass_free(otakase_ass *a) {
  if (!a) return;
  if (a->track) ass_free_track(a->track);
  ass_renderer_done(a->renderer);
  ass_library_done(a->library);
  free(a->pixels);
  free(a);
}
