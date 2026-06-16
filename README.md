# Crypto Dashboard Golang

A lightweight cryptocurrency market dashboard built with Golang.

## Overview

- Go HTTP server using net/http
- JSON handling with encoding/json
- Server-side CoinGecko API integration
- Lightweight static dashboard UI served by the Go app
- Render free-tier deployment via Docker

## Project Structure

- main.go: Go server and API proxy routes
- static/index.html: Dashboard layout
- static/styles.css: Styling
- static/app.js: Client-side rendering logic
- go.mod: Go module config
- Dockerfile: Containerized deploy/runtime
- render.yaml: Render Blueprint

## Local Development

### 1. Configure environment

```bash
cp .env.example .env
```

Optional variables:

- COINGECKO_BASE_URL (default: https://api.coingecko.com/api/v3)
- COINGECKO_API_KEY (optional)
- PORT (default: 8080)
- FRONTEND_ORIGIN (default: *)
- SNAPSHOT_PATH (default: ./db.json)
- STATIC_DIR (default: ./static)

### 2. Build and run

```bash
go run main.go
```

Open http://localhost:8080

## API Endpoints

- GET /health
- GET /api/global
- GET /api/bootstrap
- GET /api/trending
- GET /api/markets?vs_currency=usd&per_page=20&page=1
- GET /api/history?coin_id=bitcoin&days=365&vs_currency=usd

## Repository

- https://github.com/WilliamStaudenmeier/crypto-dashboard-golang
