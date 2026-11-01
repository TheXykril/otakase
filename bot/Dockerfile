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
ARG OTAKASE_HELPER_REF=main
# Straight from GitHub: the module proxy can answer "main" with a commit that
# is half an hour old, and the add-on then ships the previous helper.
RUN apk add --no-cache git
RUN mkdir /helper && CGO_ENABLED=0 GOBIN=/helper GOPRIVATE=github.com/thexykril/otakase \
    go install -trimpath -ldflags="-s -w" github.com/thexykril/otakase/cmd/otakase-helper@${OTAKASE_HELPER_REF} && \
    go version -m /helper/otakase-helper | awk '$1 == "mod" { print $3 }' > /helper-version

FROM alpine:3.22
RUN apk add --no-cache ffmpeg font-dejavu
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /otakase-bot /usr/local/bin/otakase-bot
COPY --from=build /helper/ /usr/local/bin/
COPY --from=build /helper-version /usr/local/share/otakase/helper-version
CMD ["otakase-bot"]
