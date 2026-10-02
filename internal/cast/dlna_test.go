package cast

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testDescription = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <device>
    <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
    <friendlyName>[LG] webOS TV</friendlyName>
    <UDN>uuid:1234</UDN>
    <serviceList>
      <service><serviceType>urn:schemas-upnp-org:service:ConnectionManager:1</serviceType><controlURL>/cm</controlURL></service>
    </serviceList>
    <deviceList><device>
      <serviceList>
        <service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/upnp/control/AVTransport1</controlURL></service>
        <service><serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType><controlURL>upnp/control/RenderingControl1</controlURL></service>
      </serviceList>
    </device></deviceList>
  </device>
</root>`

func TestParseRendererDescription(t *testing.T) {
	desc, err := parseRendererDescription("http://192.168.1.20:1400/desc.xml", []byte(testDescription))
	if err != nil {
		t.Fatal(err)
	}
	if desc.avTransport != "http://192.168.1.20:1400/upnp/control/AVTransport1" {
		t.Errorf("avTransport = %q", desc.avTransport)
	}
	if desc.renderingCtl != "http://192.168.1.20:1400/upnp/control/RenderingControl1" {
		t.Errorf("renderingCtl = %q", desc.renderingCtl)
	}
	device := desc.device()
	if device.Name != "[LG] webOS TV" || device.UUID != "uuid:1234" || device.Kind != KindDLNA || device.Port != 1400 || device.Addr.String() != "192.168.1.20" {
		t.Errorf("device = %+v", device)
	}
}

func TestParseRendererDescriptionNeedsAVTransport(t *testing.T) {
	if _, err := parseRendererDescription("http://x/", []byte(`<root><device><friendlyName>Speaker</friendlyName></device></root>`)); err == nil {
		t.Fatal("a renderer with no AVTransport was accepted")
	}
}

func TestSSDPHeader(t *testing.T) {
	response := "HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nLocation: http://192.168.1.20:1400/desc.xml\r\nST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n\r\n"
	if got := ssdpHeader(response, "LOCATION"); got != "http://192.168.1.20:1400/desc.xml" {
		t.Fatalf("got %q", got)
	}
}

func TestUPnPTime(t *testing.T) {
	if got := parseUPnPTime("0:23:45"); got != 1425 {
		t.Errorf("parse = %v", got)
	}
	if got := parseUPnPTime("1:02:03.500"); got != 3723.5 {
		t.Errorf("fractional = %v", got)
	}
	if got := parseUPnPTime("NOT_IMPLEMENTED"); got != 0 {
		t.Errorf("not a time = %v", got)
	}
	if got := formatUPnPTime(3723.4); got != "1:02:03" {
		t.Errorf("format = %q", got)
	}
}

func TestProgressiveURL(t *testing.T) {
	if got := progressiveURL("http://10.0.0.2:8010/g1/playlist.m3u8"); got != "http://10.0.0.2:8010/g1/stream.ts" {
		t.Fatalf("got %q", got)
	}
}

// fakeRenderer answers AVTransport calls from a script of states.
type fakeRenderer struct {
	mu      sync.Mutex
	actions []string
	state   string
	refuse  map[string]bool
}

func (f *fakeRenderer) handler(w http.ResponseWriter, r *http.Request) {
	action := r.Header.Get("SOAPAction")
	action = strings.Trim(action[strings.Index(action, "#")+1:], `"`)
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.actions = append(f.actions, action+" "+string(body))
	refuse := f.refuse[action]
	if refuse {
		delete(f.refuse, action)
	}
	state := f.state
	f.mu.Unlock()

	if refuse {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>701</errorCode><errorDescription>Transition not available</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
		return
	}
	switch action {
	case "GetTransportInfo":
		io.WriteString(w, `<s:Envelope><s:Body><u:GetTransportInfoResponse><CurrentTransportState>`+state+`</CurrentTransportState></u:GetTransportInfoResponse></s:Body></s:Envelope>`)
	case "GetPositionInfo":
		io.WriteString(w, `<s:Envelope><s:Body><u:GetPositionInfoResponse><TrackDuration>0:24:00</TrackDuration><RelTime>0:01:30</RelTime></u:GetPositionInfoResponse></s:Body></s:Envelope>`)
	case "GetVolume":
		io.WriteString(w, `<s:Envelope><s:Body><u:GetVolumeResponse><CurrentVolume>40</CurrentVolume></u:GetVolumeResponse></s:Body></s:Envelope>`)
	default:
		io.WriteString(w, `<s:Envelope><s:Body/></s:Envelope>`)
	}
}

func (f *fakeRenderer) setState(s string) {
	f.mu.Lock()
	f.state = s
	f.mu.Unlock()
}

func TestDLNAPlayerPlaysTheProgressiveStream(t *testing.T) {
	fake := &fakeRenderer{state: "STOPPED", refuse: map[string]bool{"SetAVTransportURI": true}}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer server.Close()
	p := &dlnaPlayer{name: "TV", avTransport: server.URL + "/av", renderingCtl: server.URL + "/rc"}

	if err := p.Play("http://10.0.0.2:8010/g0/playlist.m3u8"); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	actions := strings.Join(fake.actions, "\n")
	fake.mu.Unlock()
	// Refused once (701), stopped, set again, played.
	for _, want := range []string{"SetAVTransportURI", "Stop", "Play"} {
		if !strings.Contains(actions, want) {
			t.Fatalf("missing %s in:\n%s", want, actions)
		}
	}
	if !strings.Contains(actions, "<CurrentURI>http://10.0.0.2:8010/g0/stream.ts</CurrentURI>") {
		t.Fatalf("the renderer was not given the transport stream:\n%s", actions)
	}
	if !strings.Contains(actions, "&lt;DIDL-Lite") {
		t.Fatalf("metadata not escaped into the call:\n%s", actions)
	}

	// STOPPED before it has ever played is the renderer switching over.
	progress, err := p.Progress()
	if err != nil || progress.Idle {
		t.Fatalf("progress = %+v, err = %v", progress, err)
	}
	fake.setState("PLAYING")
	progress, _ = p.Progress()
	if progress.Idle || progress.Position != 90 || progress.Duration != 1440 {
		t.Fatalf("playing progress = %+v", progress)
	}
	fake.setState("STOPPED")
	if progress, _ = p.Progress(); !progress.Idle {
		t.Fatal("STOPPED after playing is the end")
	}
}

func TestDLNAPlayerVolume(t *testing.T) {
	fake := &fakeRenderer{}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer server.Close()
	p := &dlnaPlayer{name: "TV", avTransport: server.URL + "/av", renderingCtl: server.URL + "/rc"}
	p.refreshVolume()
	if p.Volume() != 0.4 {
		t.Fatalf("volume = %v", p.Volume())
	}
	if err := p.SetVolume(1.5); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	last := fake.actions[len(fake.actions)-1]
	fake.mu.Unlock()
	if !strings.Contains(last, "<DesiredVolume>100</DesiredVolume>") {
		t.Fatalf("last call = %s", last)
	}
}

// The search socket sits on the configured port, so one firewall rule covers
// the answers.
func TestListenSSDPUsesFixedPort(t *testing.T) {
	probe, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Skip("no UDP:", err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	conn, err := listenSSDP(port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := conn.LocalAddr().(*net.UDPAddr).Port; got != port {
		t.Errorf("listening on %d, want %d", got, port)
	}
}

// A taken port falls back to a random one rather than failing discovery.
func TestListenSSDPFallsBackWhenPortTaken(t *testing.T) {
	held, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Skip("no UDP:", err)
	}
	defer held.Close()
	port := held.LocalAddr().(*net.UDPAddr).Port

	conn, err := listenSSDP(port)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := conn.LocalAddr().(*net.UDPAddr).Port; got == port {
		t.Errorf("listened on the held port %d", port)
	}
}

func TestSetDiscoveryPortRejectsOutOfRange(t *testing.T) {
	t.Cleanup(func() { SetDiscoveryPort(DefaultDiscoveryPort) })
	SetDiscoveryPort(70000)
	if got := currentDiscoveryPort(); got != 0 {
		t.Errorf("port %d, want 0 (random)", got)
	}
}

// The refusals a TV gives while its "allow this device?" prompt is up are
// waited on; anything else is a real error.
func TestDLNAAwaitingApproval(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&soapRefusal{status: http.StatusUnauthorized}, true},
		{&soapRefusal{status: http.StatusForbidden}, true},
		{&soapRefusal{status: http.StatusInternalServerError, code: "606"}, true},
		{&soapRefusal{status: http.StatusInternalServerError, code: "701"}, false},
		{errors.New("something else"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := dlnaAwaitingApproval(c.err); got != c.want {
			t.Errorf("%v: got %v, want %v", c.err, got, c.want)
		}
	}
}

// Once the viewer accepts on the TV, the same order goes through, and the
// viewer was told once.
func TestAwaitApprovalRetriesUntilAllowed(t *testing.T) {
	defer func(wait, retry time.Duration) { DLNAApprovalWait, dlnaApprovalRetry = wait, retry }(DLNAApprovalWait, dlnaApprovalRetry)
	DLNAApprovalWait, dlnaApprovalRetry = time.Second, time.Millisecond
	notices := 0
	SetApprovalNotice(func(string) { notices++ })
	defer SetApprovalNotice(nil)

	tries := 0
	err := (&dlnaPlayer{name: "LG"}).awaitApproval(func() error {
		tries++
		if tries < 3 {
			return &soapRefusal{status: http.StatusUnauthorized}
		}
		return nil
	})
	if err != nil || tries != 3 || notices != 1 {
		t.Errorf("err %v, tries %d, notices %d", err, tries, notices)
	}
}

// A TV nobody answers gives up when the wait runs out, saying what to do.
func TestAwaitApprovalGivesUp(t *testing.T) {
	defer func(wait, retry time.Duration) { DLNAApprovalWait, dlnaApprovalRetry = wait, retry }(DLNAApprovalWait, dlnaApprovalRetry)
	DLNAApprovalWait, dlnaApprovalRetry = 20*time.Millisecond, time.Millisecond
	err := (&dlnaPlayer{name: "LG"}).awaitApproval(func() error {
		return &soapRefusal{status: http.StatusForbidden}
	})
	if err == nil || !strings.Contains(err.Error(), "accept it on the TV") {
		t.Errorf("err %v", err)
	}
}

// A real refusal is returned at once, without waiting or telling the viewer.
func TestAwaitApprovalReturnsOtherErrors(t *testing.T) {
	notices := 0
	SetApprovalNotice(func(string) { notices++ })
	defer SetApprovalNotice(nil)
	want := &soapRefusal{status: http.StatusInternalServerError, code: "714"}
	err := (&dlnaPlayer{name: "LG"}).awaitApproval(func() error { return want })
	if !errors.Is(err, want) || notices != 0 {
		t.Errorf("err %v, notices %d", err, notices)
	}
}
