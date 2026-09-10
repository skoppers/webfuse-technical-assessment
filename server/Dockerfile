# syntax=docker/dockerfile:1
# Build context is the repo root:  docker build -f server/Dockerfile .

# Placeholder until the dashboard build lands: copies the stub page through.
# The Vue build will run `npm ci && npm run build` here.
FROM node:20-alpine AS web
WORKDIR /web
COPY server/web/ ./

# Compile the server with the web assets embedded.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /web/ ./web/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Static binary only; migrations are embedded, so nothing else is copied.
# The nonroot base image runs the process as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
