# Used by the Home Assistant add-on (config.yaml); runs anywhere Docker does.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go config.yaml CHANGELOG.md ./
COPY assets ./assets
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /otakase-bot .
# Build the CLI from main so watch-room features match the bot after merge.
# Fall back to the released CLI if the source build is unavailable.
RUN mkdir /cli && case "$(uname -m)" in aarch64) a=arm64 ;; *) a=x86_64 ;; esac && \
    { CGO_ENABLED=0 GOBIN=/cli go install -trimpath -ldflags="-s -w -X main.version=main" \
        github.com/thexykril/otakase/cmd/otakase@main || \
      { rm -f /cli/otakase && wget -qO /cli/otakase \
          "https://github.com/TheXykril/otakase/releases/latest/download/otakase-linux-$a"; } || \
      { rm -f /cli/otakase; echo "otakase CLI unavailable"; }; } && \
    { [ ! -f /cli/otakase ] || chmod +x /cli/otakase; }

FROM alpine:3.22
RUN apk add --no-cache ffmpeg
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /otakase-bot /usr/local/bin/otakase-bot
COPY --from=build /cli/ /usr/local/bin/
CMD ["otakase-bot"]
