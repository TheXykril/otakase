package internal

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// aniSkipWriteBase is a variable so tests can point the write calls at a local
// server; nothing else replaces it.
var aniSkipWriteBase = "https://api.aniskip.com/v2/skip-times"

const (
	aniSkipWriteTimeout  = 20 * time.Second
	aniSkipSubmitterFile = "aniskip_submitter_id"

	// aniSkipProviderName is the "providerName" recorded with a submission. It
	// is the program, not the streaming source: AniSkip uses it to tell where
	// an entry came from, and blaming a provider for times this program's user
	// marked would be wrong.
	aniSkipProviderName = "otakase"

	// A span shorter than this is a mis-press rather than an opening, and one
	// longer is a marked point someone forgot to close. Neither belongs in a
	// database everyone reads.
	minimumSkipSpan = 5 * time.Second
	maximumSkipSpan = 10 * time.Minute
)

// SkipSubmission is one opening or ending offered to AniSkip.
type SkipSubmission struct {
	MalID         int
	Episode       int
	SkipType      string
	StartTime     float64
	EndTime       float64
	EpisodeLength float64
}

// Validate reports whether a submission is worth sending.
//
// Everything here is checked before anything leaves the machine, because these
// entries are public: every player that reads AniSkip will use what this sends,
// and a wrong entry is worse for them than a missing one.
func (s SkipSubmission) Validate() error {
	switch {
	case s.MalID <= 0:
		return fmt.Errorf("no MyAnimeList id for this show, which is what AniSkip files times under")
	case s.Episode <= 0:
		return fmt.Errorf("no episode number")
	case !isAniSkipType(s.SkipType):
		return fmt.Errorf("%q is not a skip type AniSkip accepts", s.SkipType)
	case s.EpisodeLength <= 0:
		return fmt.Errorf("the episode length is unknown, and AniSkip records it with every entry")
	case s.StartTime < 0:
		return fmt.Errorf("the start is before the episode begins")
	case s.EndTime <= s.StartTime:
		return fmt.Errorf("the end is not after the start")
	case s.EndTime > s.EpisodeLength+1:
		return fmt.Errorf("the end is past the end of the episode")
	}

	span := time.Duration((s.EndTime - s.StartTime) * float64(time.Second))
	if span < minimumSkipSpan {
		return fmt.Errorf("%s is too short to be an opening or an ending", formatSkipDuration(span))
	}
	if span > maximumSkipSpan {
		return fmt.Errorf("%s is too long to be an opening or an ending", formatSkipDuration(span))
	}
	return nil
}

func isAniSkipType(skipType string) bool {
	switch skipType {
	case "op", "ed", "mixed-op", "mixed-ed", "recap":
		return true
	}
	return false
}

func formatSkipDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// FormatSkipTimestamp writes a position the way a player shows it.
func FormatSkipTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds + 0.5)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

type aniSkipSubmitRequest struct {
	SkipType      string  `json:"skipType"`
	ProviderName  string  `json:"providerName"`
	StartTime     float64 `json:"startTime"`
	EndTime       float64 `json:"endTime"`
	EpisodeLength float64 `json:"episodeLength"`
	SubmitterID   string  `json:"submitterId"`
}

type aniSkipWriteResponse struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
	SkipID     string `json:"skipId"`
}

// SubmitSkipTime offers one skip time to AniSkip and returns the id it was
// filed under.
func SubmitSkipTime(storagePath string, submission SkipSubmission) (string, error) {
	if err := submission.Validate(); err != nil {
		return "", err
	}

	payload, err := json.Marshal(aniSkipSubmitRequest{
		SkipType:      submission.SkipType,
		ProviderName:  aniSkipProviderName,
		StartTime:     submission.StartTime,
		EndTime:       submission.EndTime,
		EpisodeLength: submission.EpisodeLength,
		SubmitterID:   AniSkipSubmitterID(storagePath),
	})
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/%d/%d", aniSkipWriteBase, submission.MalID, submission.Episode)
	answer, err := postAniSkipJSON(url, payload)
	if err != nil {
		return "", err
	}
	return answer.SkipID, nil
}

// VoteSkipTime agrees or disagrees with an entry someone already submitted.
func VoteSkipTime(skipID, voteType string) error {
	skipID = strings.TrimSpace(skipID)
	if skipID == "" {
		return fmt.Errorf("no skip time to vote on")
	}
	if voteType != "upvote" && voteType != "downvote" {
		return fmt.Errorf("%q is not a vote", voteType)
	}

	payload, err := json.Marshal(map[string]string{"voteType": voteType})
	if err != nil {
		return err
	}
	_, err = postAniSkipJSON(aniSkipWriteBase+"/vote/"+skipID, payload)
	return err
}

// aniSkipWriteClient is a variable so tests can point the write calls at a
// local server; nothing else replaces it.
var aniSkipWriteClient = &http.Client{Timeout: aniSkipWriteTimeout}

func postAniSkipJSON(url string, payload []byte) (aniSkipWriteResponse, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return aniSkipWriteResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := aniSkipWriteClient.Do(req)
	if err != nil {
		return aniSkipWriteResponse{}, fmt.Errorf("aniskip: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return aniSkipWriteResponse{}, err
	}

	var answer aniSkipWriteResponse
	// The body is the better error: AniSkip explains a rejection there, and a
	// bare status code would turn "these times overlap an existing entry" into
	// an opaque 400.
	_ = json.Unmarshal(body, &answer)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if message := strings.TrimSpace(answer.Message); message != "" {
			return answer, fmt.Errorf("aniskip: %s", message)
		}
		return answer, fmt.Errorf("aniskip: status %d", resp.StatusCode)
	}
	return answer, nil
}

var (
	aniSkipSubmitterMu sync.Mutex
	aniSkipSubmitterID string
)

// AniSkipSubmitterID is the identity AniSkip files submissions under.
//
// It is a UUID this program invents and keeps, not an account: AniSkip has no
// sign-up, and uses it only to group one contributor's entries. It lives in the
// storage directory so submissions stay attributable across runs, and it is
// invented rather than derived so it says nothing about the machine or user.
func AniSkipSubmitterID(storagePath string) string {
	aniSkipSubmitterMu.Lock()
	defer aniSkipSubmitterMu.Unlock()

	if aniSkipSubmitterID != "" {
		return aniSkipSubmitterID
	}

	path := ""
	if trimmed := strings.TrimSpace(os.ExpandEnv(storagePath)); trimmed != "" {
		path = filepath.Join(trimmed, aniSkipSubmitterFile)
		if data, err := os.ReadFile(path); err == nil {
			if stored := strings.TrimSpace(string(data)); stored != "" {
				aniSkipSubmitterID = stored
				return stored
			}
		}
	}

	created := newUUIDv4()
	aniSkipSubmitterID = created
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			if err := os.WriteFile(path, []byte(created+"\n"), 0o600); err != nil {
				Log(fmt.Sprintf("aniskip: could not save the submitter id: %v", err))
			}
		}
	}
	return created
}

func newUUIDv4() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// A UUID that is not random enough is still a usable label here: it
		// groups submissions and nothing more.
		return fmt.Sprintf("%016x-0000-4000-8000-%012x", time.Now().UnixNano(), time.Now().UnixNano()&0xffffffffffff)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
