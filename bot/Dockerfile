# Used by the Home Assistant add-on (config.yaml); runs anywhere Docker does.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go config.yaml CHANGELOG.md ./
COPY assets ./assets
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /otakase-bot .
# Add-on builds use main; branch validation can pin the helper to the PR ref.
# Fail the build if the matching helper cannot be built.
ARG OTAKASE_CLI_REF=main
RUN mkdir /cli && CGO_ENABLED=0 GOBIN=/cli go install -trimpath -ldflags="-s -w" \
    github.com/thexykril/otakase/cmd/otakase@${OTAKASE_CLI_REF}

FROM alpine:3.22
RUN apk add --no-cache ffmpeg font-dejavu
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /otakase-bot /usr/local/bin/otakase-bot
COPY --from=build /cli/ /usr/local/bin/
CMD ["otakase-bot"]
