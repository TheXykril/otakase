# Used by the Home Assistant add-on (config.yaml); runs anywhere Docker does.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /otakase-bot .

FROM alpine:3.22
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /otakase-bot /usr/local/bin/otakase-bot
CMD ["otakase-bot"]
