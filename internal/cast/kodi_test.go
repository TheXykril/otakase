package cast

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeKodi answers the JSON-RPC calls the player makes, the way Kodi 20 does.
type fakeKodi struct {
	mu       sync.Mutex
	user     string
	password string
	playing  bool
	paused   bool
	position float64
	volume   int
	opened   string
	seekedTo float64
	// oldSeek makes Player.Seek reject the Kodi 19+ form, as Kodi 18 does.
	oldSeek bool
	methods []string
}

func (k *fakeKodi) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if k.user != "" {
		user, password, ok := r.BasicAuth()
		if !ok || user != k.user || password != k.password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	var req struct {
		Method string                     `json:"method"`
		Params map[string]json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	k.methods = append(k.methods, req.Method)

	var result any = "OK"
	var rpcErr map[string]any
	switch req.Method {
	case "JSONRPC.Ping":
		result = "pong"
	case "XBMC.GetInfoLabels":
		result = map[string]string{"System.FriendlyName": "Living Room Kodi"}
	case "Player.Open":
		var item struct {
			File string `json:"file"`
		}
		json.Unmarshal(req.Params["item"], &item)
		k.opened, k.playing, k.position = item.File, true, 0
	case "Player.GetActivePlayers":
		if k.playing {
			result = []map[string]any{{"playerid": 1, "type": "video"}}
		} else {
			result = []any{}
		}
	case "Player.GetProperties":
		result = map[string]any{"time": kodiTimeOf(k.position), "totaltime": kodiTimeOf(1420)}
	case "Player.Seek":
		var value map[string]json.RawMessage
		json.Unmarshal(req.Params["value"], &value)
		raw, wrapped := value["time"]
		if wrapped && k.oldSeek {
			rpcErr = map[string]any{"code": -32602, "message": "Invalid params."}
			break
		}
		if !wrapped {
			raw, _ = json.Marshal(value)
		}
		var t kodiTime
		json.Unmarshal(raw, &t)
		k.seekedTo = t.seconds()
	case "Player.PlayPause":
		var play bool
		json.Unmarshal(req.Params["play"], &play)
		k.paused = !play
	case "Player.Stop":
		k.playing = false
	case "Application.GetProperties":
		result = map[string]int{"volume": k.volume}
	case "Application.SetVolume":
		json.Unmarshal(req.Params["volume"], &k.volume)
	default:
		rpcErr = map[string]any{"code": -32601, "message": "Method not found."}
	}

	response := map[string]any{"jsonrpc": "2.0", "id": 1}
	if rpcErr != nil {
		response["error"] = rpcErr
	} else {
		response["result"] = result
	}
	json.NewEncoder(w).Encode(response)
}

func startFakeKodi(t *testing.T, kodi *fakeKodi) Device {
	t.Helper()
	srv := httptest.NewServer(kodi)
	t.Cleanup(srv.Close)
	host, portText, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	return kodiDevice("Kodi", net.ParseIP(host), port)
}

func TestKodiPlayerDrivesPlayback(t *testing.T) {
	kodi := &fakeKodi{volume: 40}
	device := startFakeKodi(t, kodi)

	player, err := Connect(device)
	if err != nil {
		t.Fatal(err)
	}
	if got := player.Volume(); got != 0.4 {
		t.Fatalf("volume = %v, want 0.4", got)
	}

	// Before the stream opens nothing is playing, and that is not the end.
	if progress, err := player.Progress(); err != nil || progress.Idle {
		t.Fatalf("before play: %+v, %v", progress, err)
	}

	if err := player.Play("http://10.0.0.2:8000/playlist.m3u8"); err != nil {
		t.Fatal(err)
	}
	// Kodi joins a growing HLS playlist at its newest segment; it gets the
	// plain stream instead.
	if kodi.opened != "http://10.0.0.2:8000/"+ProgressiveName {
		t.Fatalf("opened %q", kodi.opened)
	}

	kodi.mu.Lock()
	kodi.position = 61.5
	kodi.mu.Unlock()
	progress, err := player.Progress()
	if err != nil || progress.Position != 61.5 || progress.Duration != 1420 || progress.Idle {
		t.Fatalf("progress = %+v, %v", progress, err)
	}

	if err := player.SeekToTime(90); err != nil || kodi.seekedTo != 90 {
		t.Fatalf("seek: %v, kodi at %v", err, kodi.seekedTo)
	}
	if err := player.Pause(); err != nil || !kodi.paused {
		t.Fatalf("pause: %v", err)
	}
	if err := player.Unpause(); err != nil || kodi.paused {
		t.Fatalf("unpause: %v", err)
	}
	if err := player.SetVolume(0.75); err != nil || kodi.volume != 75 || player.Volume() != 0.75 {
		t.Fatalf("volume: %v, kodi at %d", err, kodi.volume)
	}

	if err := player.Stop(); err != nil {
		t.Fatal(err)
	}
	// Having played, nothing playing means the episode is over.
	if progress, err := player.Progress(); err != nil || !progress.Idle {
		t.Fatalf("after stop: %+v, %v", progress, err)
	}
}

func TestKodiSeekFallsBackForKodi18(t *testing.T) {
	kodi := &fakeKodi{oldSeek: true}
	player, err := Connect(startFakeKodi(t, kodi))
	if err != nil {
		t.Fatal(err)
	}
	if err := player.Play("http://10.0.0.2:8000/playlist.m3u8"); err != nil {
		t.Fatal(err)
	}
	if err := player.SeekToTime(125.25); err != nil {
		t.Fatal(err)
	}
	if kodi.seekedTo != 125.25 {
		t.Fatalf("seeked to %v", kodi.seekedTo)
	}
}

func TestKodiLogin(t *testing.T) {
	kodi := &fakeKodi{user: "kodi", password: "secret"}
	device := startFakeKodi(t, kodi)

	SetKodi(KodiSettings{})
	t.Cleanup(func() { SetKodi(KodiSettings{}) })
	if _, err := Connect(device); err == nil || !strings.Contains(err.Error(), "KodiPassword") {
		t.Fatalf("expected a login hint, got %v", err)
	}

	SetKodi(KodiSettings{User: "kodi", Password: "secret"})
	if _, err := Connect(device); err != nil {
		t.Fatal(err)
	}
}

// A Kodi named in KodiHost is offered by its own name once it answers, and an
// address that answers nothing is left out rather than failing discovery.
func TestDiscoverKodiConfiguredHosts(t *testing.T) {
	kodi := &fakeKodi{}
	device := startFakeKodi(t, kodi)
	hostPort := net.JoinHostPort(device.Addr.String(), strconv.Itoa(device.Port))

	previous := kodiBrowse
	kodiBrowse = func(ctx context.Context) ([]Device, error) {
		// The same instance also announcing itself is listed once.
		return []Device{device}, nil
	}
	t.Cleanup(func() { kodiBrowse = previous })
	SetKodi(KodiSettings{Hosts: []string{" " + hostPort + " ", "", "127.0.0.1:1"}})
	t.Cleanup(func() { SetKodi(KodiSettings{}) })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	devices, err := discoverKodi(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected one Kodi, got %+v", devices)
	}
	if devices[0].Kind != KindKodi || !strings.HasSuffix(devices[0].String(), "· Kodi") {
		t.Fatalf("unexpected device %+v (%s)", devices[0], devices[0])
	}
}

func TestKodiTime(t *testing.T) {
	for _, seconds := range []float64{0, 1.5, 59.999, 3725.25} {
		got := kodiTimeOf(seconds).seconds()
		if got < seconds-0.001 || got > seconds+0.001 {
			t.Fatalf("kodiTimeOf(%v) round-trips to %v", seconds, got)
		}
	}
	if got := kodiTimeOf(-4); got != (kodiTime{}) {
		t.Fatalf("negative time = %+v", got)
	}
}
