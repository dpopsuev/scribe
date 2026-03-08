FROM golang:1.25-alpine AS build
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /scribe ./cmd/scribe

FROM scratch
COPY --from=build /scribe /scribe
ENV SCRIBE_TRANSPORT=http
ENV SCRIBE_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/scribe", "serve"]
