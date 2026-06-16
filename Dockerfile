FROM golang:1.26-alpine AS build
RUN apk add --no-cache git
WORKDIR /build
COPY go.mod go.sum ./
RUN GONOSUMDB=github.com/dpopsuev GONOSUMCHECK=github.com/dpopsuev GOPROXY=https://proxy.golang.org,direct go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o /scribe ./cmd/scribe

FROM scratch
COPY --from=build /scribe /scribe
COPY --from=build /build/web/templates /web/templates
COPY --from=build /build/web/static /web/static
ENV HOME=/data
ENV SCRIBE_ROOT=/data
ENV SCRIBE_TRANSPORT=http
ENV SCRIBE_ADDR=:8080
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/scribe", "serve"]
