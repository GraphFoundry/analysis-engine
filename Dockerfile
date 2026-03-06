# ---------- Build stage ----------
FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux \
    go build -ldflags="-s -w" \
    -o app ./cmd/analysis-engine


# ---------- Runtime stage ----------
FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /app/app .

EXPOSE 5000

USER nonroot:nonroot

CMD ["./app"]