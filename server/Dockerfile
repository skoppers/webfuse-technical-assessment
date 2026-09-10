# syntax=docker/dockerfile:1
# Build context is the repo root:  docker build -f server/Dockerfile .

# Compile the server with the static dashboard (server/web/) embedded. No Node stage:
# the dashboard is plain HTML + JS with no build step.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Static binary only; migrations are embedded, so nothing else is copied.
# The nonroot base image runs the process as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
