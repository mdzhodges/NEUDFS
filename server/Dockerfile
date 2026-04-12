# Build stage
FROM golang:1.26.1-alpine AS builder


WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN GOARCH=amd64 GOOS=linux go build -o server ./server

FROM --platform=linux/amd64 alpine:latest
RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/server .

EXPOSE 50051

CMD ["./server"]