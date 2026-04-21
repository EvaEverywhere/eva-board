FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/migrate ./cmd/migrate

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata curl

COPY --from=builder /bin/server /bin/server
COPY --from=builder /bin/migrate /bin/migrate

LABEL org.opencontainers.image.licenses=Apache-2.0

EXPOSE 8080

CMD ["/bin/server"]
