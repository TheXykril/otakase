package internal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// diagLogLines is how much of the log `otakase -diag` shows: the error a
// support post is about is near the end.
const diagLogLines = 30

// WantsDiagnostics reports whether -diag or --diag is among the arguments.
// It is checked before the log is cleared at launch, so the block can show
// the previous run's log.
func WantsDiagnostics(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-diag" || a == "--diag" {
			return true
		}
	}
	return false
}

// Diagnostics is the block `otakase -diag` prints for pasting into a support
// post: version, system, mpv, where the config and log are, and the end of
// the log with tokens and passwords masked.
func Diagnostics(version, configPath, logPath string) string {
	var sb strings.Builder
	sb.WriteString("```\n")
	sb.WriteString("otakase diagnostics\n")
	fmt.Fprintf(&sb, "%s version: %s\n", DisplayName, version)
	fmt.Fprintf(&sb, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&sb, "mpv: %s\n", mpvVersionLine())
	fmt.Fprintf(&sb, "Config: %s\n", configPath)
	fmt.Fprintf(&sb, "Log: %s\n", logPath)
	secrets := configSecrets(configPath)
	lines := logTail(logPath, diagLogLines)
	fmt.Fprintf(&sb, "--- last %d log lines ---\n", len(lines))
	for _, line := range lines {
		line = strings.ReplaceAll(maskSecrets(line, secrets), "```", "'''")
		sb.WriteString(line + "\n")
	}
	sb.WriteString("```\n")
	return sb.String()
}

// mpvVersionLine is the first line of `mpv --version`, or "not found".
func mpvVersionLine() string {
	path, err := exec.LookPath("mpv")
	if err != nil {
		return "not found"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if err != nil && first == "" {
		return "not found"
	}
	return strings.TrimSpace(first)
}

// logTail returns the last n non-empty lines of a file. Only the end of a
// large file is read.
func logTail(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	const window = 256 << 10
	if fi, err := f.Stat(); err == nil && fi.Size() > window {
		_, _ = f.Seek(fi.Size()-window, io.SeekStart)
	}
	data, _ := io.ReadAll(f)
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// secretKeyRe matches config keys whose value must not be shared.
var secretKeyRe = regexp.MustCompile(`(?i)token|secret|key|password`)

// configSecrets returns the values of secret config keys, so they can be
// masked wherever they show up in the log. Short and on/off values are left
// out: masking "true" everywhere would hide the log, not a secret.
func configSecrets(configPath string) []string {
	values, err := LoadConfigFromFile(configPath)
	if err != nil {
		return nil
	}
	var out []string
	for k, v := range values {
		v = strings.Trim(v, `"'`)
		if !secretKeyRe.MatchString(k) || len(v) < 6 || strings.EqualFold(v, "true") || strings.EqualFold(v, "false") {
			continue
		}
		out = append(out, v)
	}
	// Longest first, so a secret that contains another is masked whole.
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

var (
	// secretFieldRe finds key=value, key: value and "key":"value" pairs whose
	// key names a token, secret, key or password.
	secretFieldRe = regexp.MustCompile(`(?i)([A-Za-z0-9_.-]*(?:token|secret|key|password)[A-Za-z0-9_.-]*"?\s*[:=]\s*"?)([^\s"',&;]{4,})`)
	bearerRe      = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]{8,}`)
)

const masked = "****"

// maskSecrets hides the given secret values and anything that looks like a
// token, secret, key or password in a line.
func maskSecrets(line string, secrets []string) string {
	for _, s := range secrets {
		line = strings.ReplaceAll(line, s, masked)
	}
	line = bearerRe.ReplaceAllString(line, "${1}"+masked)
	return secretFieldRe.ReplaceAllStringFunc(line, func(m string) string {
		sub := secretFieldRe.FindStringSubmatch(m)
		if sub[2] == masked {
			return m
		}
		return sub[1] + masked
	})
}
