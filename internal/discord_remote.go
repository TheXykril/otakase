package internal

import (
	"fmt"
	"sync"
)

// /remote in Discord: pause, play, skip or change the episode playing in
// this app. The bot sends a "remote" item that expires in half a minute;
// when an mpv playback runs in this process the action goes to it over
// IPC, otherwise it is dropped. Next and previous episode are mpv's
// playlist-next/prev, the same as the player's own buttons, which the
// episode playlist turns into an episode switch.

// remotePlayer is the mpv socket of the playback running now, set while
// StartDiscordMPVWatch follows it.
var remotePlayer struct {
	sync.Mutex
	socket string
}

func init() {
	registerDiscordInbox("remote", applyDiscordRemote)
}

// trackRemotePlayer notes the playback /remote acts on.
func trackRemotePlayer(socket string) {
	remotePlayer.Lock()
	remotePlayer.socket = socket
	remotePlayer.Unlock()
}

// untrackRemotePlayer forgets a playback that ended, unless another one
// took its place meanwhile.
func untrackRemotePlayer(socket string) {
	remotePlayer.Lock()
	if remotePlayer.socket == socket {
		remotePlayer.socket = ""
	}
	remotePlayer.Unlock()
}

func currentRemotePlayer() string {
	remotePlayer.Lock()
	defer remotePlayer.Unlock()
	return remotePlayer.socket
}

// remoteCommand is the mpv command for an action and the line shown on the
// player; ok is false for an action this app doesn't know.
func remoteCommand(action string) (command []interface{}, osd string, ok bool) {
	switch action {
	case "pause":
		return []interface{}{"set_property", "pause", true}, "Paused from Discord", true
	case "play":
		return []interface{}{"set_property", "pause", false}, "Playing from Discord", true
	case "skip":
		return []interface{}{"seek", 85, "relative"}, "Skipped 85 s from Discord", true
	case "next":
		return []interface{}{"playlist-next", "force"}, "Next episode from Discord", true
	case "previous":
		return []interface{}{"playlist-prev", "force"}, "Previous episode from Discord", true
	case "stop":
		return []interface{}{"quit"}, "", true
	}
	return nil, "", false
}

// mpvRemoteSend is MPVSendCommand; tests replace it.
var mpvRemoteSend = MPVSendCommand

// applyDiscordRemote runs a /remote action on the playback, if any.
func applyDiscordRemote(config *Config, item discordInboxItem) {
	command, osd, ok := remoteCommand(item.Key)
	if !ok {
		Log(fmt.Sprintf("Discord: unknown remote action %q", item.Key))
		return
	}
	socket := currentRemotePlayer()
	if socket == "" {
		Log(fmt.Sprintf("Discord: remote %s ignored, nothing is playing", item.Key))
		return
	}
	if osd != "" {
		if _, err := mpvRemoteSend(socket, []interface{}{"show-text", osd, 2000}); err != nil {
			Log("Discord: remote: " + err.Error())
		}
	}
	if _, err := mpvRemoteSend(socket, command); err != nil {
		Log(fmt.Sprintf("Discord: remote %s: %v", item.Key, err))
		return
	}
	Log(fmt.Sprintf("Discord: remote %s applied", item.Key))
}
