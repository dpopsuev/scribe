FROM node:22-alpine AS frontend
RUN corepack enable && corepack prepare pnpm@latest --activate
WORKDIR /app
COPY web/frontend/package.json web/frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY web/frontend/ .
RUN pnpm run build

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
COPY --from=frontend /app/build /web/frontend/build
ENV HOME=/data
ENV SCRIBE_ROOT=/data
ENV SCRIBE_TRANSPORT=http
ENV SCRIBE_ADDR=:8080
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/scribe", "serve"]
