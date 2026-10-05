# Used by the Home Assistant add-on (config.yaml); runs anywhere Docker does.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /otakase-bot .
# The latest otakase release, for /provider-status.
RUN case "$(uname -m)" in aarch64) a=arm64 ;; *) a=x86_64 ;; esac && \
    wget -qO /otakase "https://github.com/TheXykril/otakase/releases/latest/download/otakase-linux-$a" && \
    chmod +x /otakase

FROM alpine:3.22
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /otakase-bot /usr/local/bin/otakase-bot
COPY --from=build /otakase /usr/local/bin/otakase
CMD ["otakase-bot"]
