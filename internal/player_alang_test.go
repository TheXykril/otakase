package internal

import "testing"

// A host serving every dub in one stream marks Japanese default, and mpv plays
// the default unless told which language to prefer: a dub request heard
// Japanese. The preference follows the mode that resolved the episode.
func TestMPVAudioLanguageArgsFollowTheMode(t *testing.T) {
	if got := mpvAudioLanguageArgs("dub", nil); len(got) != 1 || got[0] != "--alang="+mpvDubLanguages {
		t.Errorf("dub: %v", got)
	}
	if got := mpvAudioLanguageArgs("sub", nil); len(got) != 1 || got[0] != "--alang="+mpvSubLanguages {
		t.Errorf("sub: %v", got)
	}
}

// A viewer's own --alang in MpvArgs wins.
func TestMPVAudioLanguageArgsLeaveTheViewersChoice(t *testing.T) {
	if got := mpvAudioLanguageArgs("dub", []string{"--alang=de"}); len(got) != 0 {
		t.Errorf("overrode the viewer's --alang: %v", got)
	}
}
