package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Staff hear about a background job failing at most this often per job.
const alertEvery = 6 * time.Hour

// health remembers which background jobs are failing, so staff get one note
// when a job breaks, none while it keeps failing, and one when it works again.
type health struct {
	mu      sync.Mutex
	last    map[string]time.Time // job → last alert
	failing map[string]bool      // jobs staff were told about
	now     func() time.Time

	jobs map[string]*jobStatus // what each job last reported, for /bot-status
}

func newHealth() *health {
	return &health{last: map[string]time.Time{}, failing: map[string]bool{}, now: time.Now}
}

// fail records a failure and reports whether staff should be told.
func (h *health) fail(job string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	if t, ok := h.last[job]; ok && now.Sub(t) < alertEvery {
		return false
	}
	h.last[job] = now
	h.failing[job] = true
	return true
}

// ok records a success and reports whether staff should hear the job
// recovered.
func (h *health) ok(job string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	was := h.failing[job]
	delete(h.failing, job)
	return was
}

// jobFailed logs a background job's error and tells staff, rate limited.
func (b *bot) jobFailed(job string, err error) {
	log.Printf("%s: %v", job, err)
	b.health.noteFail(job, err)
	if b.health.fail(job) {
		b.staffNote(&discordgo.MessageEmbed{
			Title:       job + " failed",
			Description: clip(err.Error(), 1500),
			Color:       shu,
			Footer:      &discordgo.MessageEmbedFooter{Text: "Muted for this job for 6 hours. The bot keeps retrying."},
		})
	}
}

// jobOK notes a background job worked, telling staff when it was failing.
func (b *bot) jobOK(job string) {
	b.health.noteOK(job)
	if b.health.ok(job) {
		b.staffNote(&discordgo.MessageEmbed{Title: job + " is working again", Color: 0x3BA55C})
	}
}

// keepRunning runs a background loop, restarting it a minute after a panic
// instead of letting it take the whole bot down.
func (b *bot) keepRunning(job string, loop func()) {
	b.health.noteStart(job)
	for {
		if !b.runGuarded(job, loop) {
			b.health.noteStop(job)
			return
		}
		time.Sleep(time.Minute)
	}
}

// runGuarded runs loop and reports whether it panicked.
func (b *bot) runGuarded(job string, loop func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("%s panic: %v\n%s", job, r, debug.Stack())
			b.jobFailed(job, fmt.Errorf("crashed: %v", r))
			panicked = true
		}
	}()
	loop()
	return false
}

// staffNote posts to the bot log channel, else mod-chat.
func (b *bot) staffNote(e *discordgo.MessageEmbed) {
	ch := b.cfg.LogChannel
	if ch == "" {
		ch = b.cfg.ModChannel
	}
	if ch == "" || b.s == nil {
		return
	}
	if _, err := b.s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{e}, AllowedMentions: &discordgo.MessageAllowedMentions{},
	}); err != nil {
		log.Printf("staff note: %v", err)
	}
}

// crashWatch notices when the previous run ended without a clean shutdown.
// A marker file exists while the bot runs and is removed on SIGINT/SIGTERM;
// Go writes a fatal panic's output to the crash file.
type crashWatch struct {
	marker, crashFile string
	crashed           bool   // the previous run did not shut down cleanly
	output            string // its panic output, if it panicked
}

func newCrashWatch(dir string) *crashWatch {
	return &crashWatch{
		marker:    filepath.Join(dir, "otakase-bot.running"),
		crashFile: filepath.Join(dir, "otakase-bot.crash"),
	}
}

// start checks the previous run, then arms the marker and crash output.
func (c *crashWatch) start() {
	if _, err := os.Stat(c.marker); err == nil {
		c.crashed = true
	}
	if b, err := os.ReadFile(c.crashFile); err == nil {
		c.output = strings.TrimSpace(string(b))
		if c.output != "" {
			c.crashed = true
		}
	}
	if err := os.WriteFile(c.marker, []byte(time.Now().UTC().Format(time.RFC3339)), 0o600); err != nil {
		log.Printf("crash marker: %v", err)
	}
	f, err := os.Create(c.crashFile)
	if err != nil {
		log.Printf("crash file: %v", err)
		return
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		log.Printf("crash output: %v", err)
	}
	f.Close() // SetCrashOutput keeps its own copy
}

// stop marks a clean shutdown.
func (c *crashWatch) stop() {
	if err := os.Remove(c.marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("crash marker: %v", err)
	}
}

// embed describes the crash for staff, or is nil after a clean shutdown.
func (c *crashWatch) embed() *discordgo.MessageEmbed {
	if !c.crashed {
		return nil
	}
	e := &discordgo.MessageEmbed{
		Title:       "Bot restarted after a crash",
		Description: "The last run stopped without shutting down cleanly (a crash, a power cut or a killed process).",
		Color:       shu,
	}
	if c.output != "" {
		lines := strings.Split(c.output, "\n")
		if len(lines) > 12 {
			lines = lines[:12]
		}
		e.Description = "The last run crashed:\n```\n" + clip(strings.Join(lines, "\n"), 1500) + "\n```"
	}
	return e
}
