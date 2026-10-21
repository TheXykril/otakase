package cast

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Several providers serve one HLS stream carrying every dub the host has --
// Hindi, Tamil, English, Japanese and more -- with Japanese marked default.
// ffmpeg, left to itself, casts the default, so a viewer who asked for the dub
// heard Japanese under burned English subtitles. The stream says which track
// is which, so the cast reads that and asks for the one it wants.

// AudioTrack is one audio stream of the source, as ffprobe numbers it.
type AudioTrack struct {
	Index    int
	Language string // normalised: "en", "ja", or the code as given
	Default  bool
}

// VideoTrack is one video stream of the source; an HLS master carries one per
// variant.
type VideoTrack struct {
	Index         int
	Width, Height int
}

// SubtitleTrack is one subtitle stream inside the source, as a release from
// a torrent carries them: in the file, rather than at a URL of their own.
type SubtitleTrack struct {
	Index    int
	Codec    string // ffprobe's codec_name: "ass", "subrip", "hdmv_pgs_subtitle"...
	Language string // normalised, as for AudioTrack
	Title    string
	Default  bool
	Forced   bool
}

// StreamInfo is what one ffprobe of the source reports.
type StreamInfo struct {
	Duration  float64
	Audio     []AudioTrack
	Video     []VideoTrack
	Subtitles []SubtitleTrack
	// Fonts are the indexes of font attachments. Fansub releases carry the
	// fonts their ASS styles name; without them libass falls back to whatever
	// the system has, and signs and typesetting come out in the wrong face.
	Fonts []int
}

// TrackSelection is the video and audio a cast plays.
type TrackSelection struct {
	// Maps are ffmpeg -map arguments, empty when ffmpeg's own choice stands.
	Maps []string
	// AudioLanguage is the normalised language of the audio that plays, or ""
	// when the stream does not say.
	AudioLanguage string
}

// NormaliseLanguage folds the codes hosts use for the two languages that
// matter here onto one spelling each.
func NormaliseLanguage(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	switch code {
	case "en", "eng", "english":
		return "en"
	case "ja", "jp", "jpn", "japanese":
		return "ja"
	}
	return code
}

// ProbeStreamArgs assembles the ffprobe command that reads a stream's length
// and tracks in one pass.
func ProbeStreamArgs(streamURL, referrer string, headers map[string]string) []string {
	args := []string{"-hide_banner", "-v", "error"}

	// The same two allowances the remux needs: providers disguise HLS segments
	// as images, and ffprobe rejects unrecognised extensions exactly as ffmpeg
	// does. Without these a probe fails on the streams most in need of it.
	args = append(args, InputArgs(streamURL)...)

	// Per-input option, so it has to precede -i, the same as in BuildRemuxArgs.
	args = append(args, inputHeaderArgs(referrer, headers)...)

	args = append(args,
		"-show_entries", "format=duration:stream=index,codec_type,codec_name,width,height:stream_disposition=default,forced:stream_tags=language,title,mimetype,filename",
		"-of", "json",
	)
	return append(args, "-i", streamURL)
}

// ParseProbedStream reads ffprobe's JSON. A length ffprobe could not work out
// is left zero rather than reported as an error, for the reason
// ParseProbedDuration gives.
func ParseProbedStream(output []byte) (StreamInfo, error) {
	var raw struct {
		Streams []struct {
			Index       int    `json:"index"`
			CodecType   string `json:"codec_type"`
			CodecName   string `json:"codec_name"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
			Disposition struct {
				Default int `json:"default"`
				Forced  int `json:"forced"`
			} `json:"disposition"`
			Tags struct {
				Language string `json:"language"`
				Title    string `json:"title"`
				MimeType string `json:"mimetype"`
				Filename string `json:"filename"`
			} `json:"tags"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return StreamInfo{}, fmt.Errorf("cast: could not read the stream probe: %w", err)
	}

	var info StreamInfo
	info.Duration, _ = ParseProbedDuration(raw.Format.Duration)
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "audio":
			info.Audio = append(info.Audio, AudioTrack{
				Index:    s.Index,
				Language: NormaliseLanguage(s.Tags.Language),
				Default:  s.Disposition.Default == 1,
			})
		case "video":
			info.Video = append(info.Video, VideoTrack{Index: s.Index, Width: s.Width, Height: s.Height})
		case "subtitle":
			info.Subtitles = append(info.Subtitles, SubtitleTrack{
				Index:    s.Index,
				Codec:    s.CodecName,
				Language: NormaliseLanguage(s.Tags.Language),
				Title:    s.Tags.Title,
				Default:  s.Disposition.Default == 1,
				Forced:   s.Disposition.Forced == 1,
			})
		case "attachment":
			if isFontAttachment(s.CodecName, s.Tags.MimeType, s.Tags.Filename) {
				info.Fonts = append(info.Fonts, s.Index)
			}
		}
	}
	return info, nil
}

// ProbeStream asks ffprobe for a stream's length and tracks. Only playlists
// and the first segment of each track are fetched, not the episode.
func ProbeStream(ffprobePath, streamURL, referrer string, headers map[string]string) (StreamInfo, error) {
	out, err := exec.Command(ffprobePath, ProbeStreamArgs(streamURL, referrer, headers)...).Output()
	if err != nil {
		return StreamInfo{}, fmt.Errorf("cast: could not probe the stream: %w", err)
	}
	return ParseProbedStream(out)
}

// SelectTracks picks the audio in the wanted language, else the one the host
// marks default, else the first, and the video with the most pixels.
//
// A stream with a single audio track has nothing to choose and gets no maps:
// ffmpeg's own selection has served it all along. Naming any stream stops
// ffmpeg choosing the others, which is why the video is named too.
func SelectTracks(info StreamInfo, wantLanguage string) TrackSelection {
	if len(info.Audio) == 0 {
		return TrackSelection{}
	}
	if len(info.Audio) == 1 {
		return TrackSelection{AudioLanguage: info.Audio[0].Language}
	}

	audio := info.Audio[0]
	for _, track := range info.Audio {
		if track.Default {
			audio = track
			break
		}
	}
	for _, track := range info.Audio {
		if wantLanguage != "" && track.Language == NormaliseLanguage(wantLanguage) {
			audio = track
			break
		}
	}

	if len(info.Video) == 0 {
		return TrackSelection{AudioLanguage: audio.Language}
	}
	video := info.Video[0]
	for _, track := range info.Video[1:] {
		if track.Width*track.Height > video.Width*video.Height {
			video = track
		}
	}
	return TrackSelection{
		Maps:          []string{"-map", "0:" + strconv.Itoa(video.Index), "-map", "0:" + strconv.Itoa(audio.Index)},
		AudioLanguage: audio.Language,
	}
}

// isFontAttachment reports whether an attachment is a font. Muxers disagree on
// how to say so -- some set a codec, some only a MIME type, some only a file
// name -- so any of the three is enough.
func isFontAttachment(codec, mimeType, filename string) bool {
	switch strings.ToLower(codec) {
	case "ttf", "otf":
		return true
	}
	mimeType = strings.ToLower(mimeType)
	if strings.Contains(mimeType, "font") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".ttf", ".otf", ".ttc":
		return true
	}
	return false
}

// textSubtitleCodecs are the subtitle formats ffmpeg can turn into ASS, which
// is what the subtitles filter draws. Picture-based ones (PGS, VobSub) are not
// text at all and would need overlaying instead.
var textSubtitleCodecs = map[string]bool{
	"ass": true, "ssa": true, "subrip": true, "srt": true,
	"webvtt": true, "mov_text": true, "text": true,
}

// SelectSubtitle picks the embedded subtitle track to draw: English when the
// release labels one, else one with no language at all, which on a fansub
// release is English in practice. A "signs & songs" or forced track only
// translates on-screen text, so a full one is preferred whenever there is one,
// and among equals the track the release marks default wins.
//
// Only text tracks are considered. ok is false when there is nothing to draw,
// including when every track is in some other language.
func SelectSubtitle(info StreamInfo) (track SubtitleTrack, ok bool) {
	best := -1
	for _, candidate := range info.Subtitles {
		if !textSubtitleCodecs[strings.ToLower(candidate.Codec)] {
			continue
		}
		score := 0
		switch candidate.Language {
		case "en":
			score += 100
		case "", "und":
			score += 50
		default:
			continue
		}
		title := strings.ToLower(candidate.Title)
		if !candidate.Forced && !strings.Contains(title, "sign") && !strings.Contains(title, "song") {
			score += 20
		}
		if candidate.Default {
			score += 5
		}
		if score > best {
			best, track, ok = score, candidate, true
		}
	}
	return track, ok
}
