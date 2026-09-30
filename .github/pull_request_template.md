<!-- Use a conventional title: fix(scope): ..., feat(scope): ..., chore: ... -->

## What changes for the viewer

**Before:**

**After:**

## How

<!-- The approach in a few lines, and anything a reviewer should look at closely. -->

## Testing

- [ ] `go vet ./...`
- [ ] `go test -short -race ./...`
- [ ] Tried it for real (mpv / cast / rofi): <!-- what you ran, or why not -->

## Checklist

- [ ] README updated if a setting, flag or key binding changed
- [ ] No version bump or `release:` commit (releases are cut separately)

<!-- Related issue: Fixes # -->
