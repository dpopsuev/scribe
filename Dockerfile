FROM node:22-alpine AS frontend
WORKDIR /frontend
COPY web/frontend/package.json web/frontend/pnpm-lock.yaml ./
RUN npm install
COPY web/frontend/ .
RUN npx vite build

FROM golang:1.26-alpine AS build
RUN apk add --no-cache git
WORKDIR /build
COPY go.mod go.sum ./
RUN GONOSUMDB=github.com/dpopsuev GONOSUMCHECK=github.com/dpopsuev GOPROXY=https://proxy.golang.org,direct go mod download
COPY . .
COPY --from=frontend /frontend/build web/static/app
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o /scribe ./cmd/scribe

FROM scratch
COPY --from=build /scribe /scribe
ENV HOME=/data
ENV SCRIBE_ROOT=/data
ENV SCRIBE_TRANSPORT=http
ENV SCRIBE_ADDR=:8080
ENV SCRIBE_ID_FORMAT=scoped
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/scribe", "serve"]
