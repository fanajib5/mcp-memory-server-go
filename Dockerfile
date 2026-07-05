FROM golang:1.23-alpine AS build
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcp-memory-server ./cmd/server

# Final stage: distroless static image, no shell, minimal attack surface, tiny footprint
FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /app/mcp-memory-server .
EXPOSE 3000
USER nonroot:nonroot
ENTRYPOINT ["/app/mcp-memory-server"]
