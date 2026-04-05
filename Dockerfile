FROM golang:1.25-alpine AS build
WORKDIR /build
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o /scribe ./cmd/scribe

FROM scratch
COPY --from=build /scribe /scribe
ENV SCRIBE_ROOT=/data
ENV SCRIBE_TRANSPORT=http
ENV SCRIBE_ADDR=:8080
ENV SCRIBE_ID_FORMAT=scoped
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/scribe", "serve"]
