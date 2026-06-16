FROM golang:1.22-bookworm AS build

WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/bin/crypto-dashboard-golang ./main.go

FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
  ca-certificates \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /app/bin/crypto-dashboard-golang /app/crypto-dashboard-golang
COPY --from=build /app/static /app/static
COPY --from=build /app/db.json /app/db.json

ENV PORT=8080
EXPOSE 8080

CMD ["/app/crypto-dashboard-golang"]
