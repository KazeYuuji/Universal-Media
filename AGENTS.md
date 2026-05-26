# Project Summary

## Apa ini
Aplikasi downloader video/foto dari YouTube, Instagram, TikTok, Facebook.
Frontend Next.js, backend Go. Komunikasi via Server-Sent Events (SSE) + proxy.

## Struktur
- `frontend/` — Next.js (TypeScript, Tailwind)
- `backend/` — Go (net/http)

## Setup & Run
```bash
# Backend
cd backend && go run .
# Frontend  
cd frontend && npm run dev
```

## Deployment

### Backend (Hugging Face Spaces)
Backend butuh Docker karena depend pada yt-dlp + ffmpeg.
```bash
cd backend
docker build -t media-backend .
docker run -p 7860:7860 -e PORT=7860 media-backend
```

### Frontend (Vercel)
Set env var `NEXT_PUBLIC_BACKEND_URL` = URL Hugging Face backend

## Status
✅ YouTube — real format extraction + proxy download (9 opsi hingga 2160p 60fps)
✅ Instagram — real photo extraction via Downloadgram JWT → CDN URL (2 foto WebP)
✅ TikTok — extraction via TikWM API
✅ Facebook — extraction via og:video fallback
❌ HF Spaces — YouTube/IG block datacenter IPs (yt-dlp + CDN 403)
❌ Instagram rate-limited — Downloadgram API returns 404/400 after ~10 requests, retry with 2s delay

## Key Architecture
- **SSE**: `/api/stream/video` dan `/api/stream/image` untuk ekstraksi format
- **Proxy**: `/api/proxy?url=...&yt_format=...` untuk download aktual
- **yt-dlp**: Deteksi via PATH → WinGet → python3/python -m yt_dlp
- **Downloadgram JWT**: Decode token, ekstrak CDN URL langsung dari Instagram
- **Tanpa simulasi**: Semua opsi adalah format real, bukan URL palsu

## Security (7 Layer)
Rate limiting (30/10 req/s), SSRF protection, input validation, security headers, panic recovery, request logging, middleware chain.
