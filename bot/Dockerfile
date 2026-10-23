# Used by the Home Assistant add-on (config.yaml); runs anywhere Docker does.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go config.yaml CHANGELOG.md ./
COPY assets ./assets
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /otakase-bot .
# The otakase CLI, for /provider-status: the latest release, or a build of
# main while there is none. Never fails the image; without it the command
# just says the check isn't available.
RUN mkdir /cli && case "$(uname -m)" in aarch64) a=arm64 ;; *) a=x86_64 ;; esac && \
    { wget -qO /cli/otakase "https://github.com/TheXykril/otakase/releases/latest/download/otakase-linux-$a" || \
      { rm -f /cli/otakase && CGO_ENABLED=0 GOBIN=/cli go install -trimpath -ldflags="-s -w -X main.version=main" \
          github.com/thexykril/otakase/cmd/otakase@main; } || \
      { rm -f /cli/otakase; echo "otakase CLI unavailable, /provider-status disabled"; }; } && \
    { [ ! -f /cli/otakase ] || chmod +x /cli/otakase; }

FROM alpine:3.22
RUN apk add --no-cache ffmpeg
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /otakase-bot /usr/local/bin/otakase-bot
COPY --from=build /cli/ /usr/local/bin/
CMD ["otakase-bot"]
