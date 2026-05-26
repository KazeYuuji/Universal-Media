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
# Build & test Docker image
cd backend
docker build -t media-backend .
docker run -p 7860:7860 -e PORT=7860 media-backend
```
- Buat akun [huggingface.co](https://huggingface.co) (no credit card)
- Klik profil → **New Space**
  - Space Name: `media-backend`
  - License: `mit`
  - Space SDK: **Docker**
  - Space Hardware: **CPU free** (2 vCPU, 16GB)
- Clone Space: `git clone https://huggingface.co/spaces/{username}/media-backend`
- Copy isi folder `backend/` ke repo Space (termasuk `Dockerfile`)
- **Set PORT env**: Settings → Variables → `PORT=7860`
- Commit & push → auto build
- Dapat URL: `https://{username}-media-backend.hf.space`

### Frontend (Vercel)
- Push repo ke GitHub → import ke Vercel
- Set env var `NEXT_PUBLIC_BACKEND_URL` = URL Hugging Face backend
  (default: `http://localhost:8081`)
- Deploy otomatis tiap push

## Bug fixes done
- **Download gagal**: `triggerDownload()` tidak kirim `yt_format` ke proxy → backend fallback ke proxy langsung (gagal). Fix: tambah `params.set("yt_format", ...)` 
- **Port 8081 zombie**: proses yt-dlp menempel. Kill: `kill -9 $(ps aux | grep youtube-dl | grep -v grep | awk '{print $1}')`
- **Fragment lock Windows**: DASH format gagal karena `--no-part` + `-o -`. Fix: hapus `--no-part`, yt-dlp manage .part sendiri
- **Video-only format**: butuh ffmpeg merge. Fix: `--ffmpeg-location` ke yt-dlp, skip jika ffmpeg absent
- **Preview lambat**: ganti `<video>` jadi `<img>` thumbnail. Tombol Preview buka thumbnail
- **Download progress**: `XMLHttpRequest` + `onprogress` gantikan `fetch`
- **Ukuran file**: parse `filesize`/`filesize_approx`, fallback TBR×duration
- **Nama file**: format `Judul Video - 1080p.mp4`, kirim via `filename` query → `Content-Disposition`

## Cookieless approach (current)
- **Semua kode cookie dihapus** — tidak ada `--cookies`, `--cookies-from-browser`, atau `cookies.txt`
- **YouTube Fallback chain** (9 methods):
  1. yt-dlp `-J` — coba 9 client config: `android`, `android_vr`, `web`, `ios`, `tv_android`, `web_creator`, `android+skip=webpage`, `android+no_dash`, `default`
  2. kkdai library (10s timeout)
  3. Invidious API (coba 8 instance: projectsegfau.lt, yewtu.be, nadeko.net, slipfox.xyz, puffyan.us, odyssey346.dev, privacydev.net, baczek.me)
  4. Direct pipe `yt-dlp -f best[ext=mp4] -g` → dapat URL langsung
  5. Simulation (real title via oEmbed, sample W3Schools video)
- **YouTube dari IP rumahan**: BERHASIL — yt-dlp dengan `--js-runtimes node` + `--remote-components ejs:github` mendapat format hingga 4K 60fps, download via proxy valid MP4
- **YouTube dari HF Spaces** (datacenter IP): GAGAL — Google blokir semua request tanpa cookies dari IP datacenter
- **Instagram** (9 image extraction methods):
  1. GraphQL API (`/graphql/query`)
  2. Mobile API (`i.instagram.com/api/v1/media`)
  3. Embed JSON (Facebook UA scrape `/embed/`)
  4. Embed Scraping (Chrome UA scrape `/embed/captioned/`)
  5. Public Viewer (Picuki, Imginn)
  6. Downloadgram (`api.downloadgram.org/media`)
  7. oEmbed (`api.instagram.com/oembed`)
  8. Direct Page (scrape halaman IG langsung → og:image, JSON-LD, CDN regex)
  9. Snapinsta Images (`snapinsta.app` → photo class, data-url, href)
- **Instagram dari IP rumahan**: BERHASIL — downloadgram JWT token JPEG + CDN langsung via proxy (213KB, HTTP 200)
- **Instagram dari HF Spaces**: GAGAL — CDN blokir IP datacenter (403)
- **TikTok, Facebook** — tetap berfungsi normal (tidak terblokir)
- **Jika suatu saat platform longgar**, download akan kembali berfungsi otomatis tanpa perubahan kode.

## Bug fixes done (cont.)
- **Instagram video campur image**: downloadgram API return 2 token (image `.jpg` + video `.mp4`), keduanya dilabel "mp4". User download image → error "Cannot read image.png". Fix: `isDownloadgramVideoToken()` decode JWT payload, cek filename extension, skip token dengan `.jpg`
- **Snapinsta data-url ambiguitas**: `data-url` regex tangkap image & video URL. Fix: filter hanya URL dengan `.mp4`, `/video/`, atau `video_dashinit`
- **Instagram video tanpa filter video**: `fetchInstagramVideos()` tidak filter option. Fix: panggil `filterVideoOptions()` di semua 3 method
- **detectExtFromURL path-based**: sebelumnya pakai `strings.Contains(lowerURL, ".mp4")` yang false-positive kalo query param mengandung `.mp4` (ex: `...jpg?fm=mp4`). Fix: parse URL, cek `path.Ext(parsed.Path)` — extension di path, bukan query string
- **filterVideoOptions selalu cek URL**: sebelumnya skip URL check kalo `Format == "mp4"`. Fix: selalu panggil `detectExtFromURL` — kalo balikin image extension (jpg/png), skip option tsb
- **Downloadgram reVideo path check**: regex `.mp4` bisa match query param. Fix: hapus `.mp4` dari regex, validasi path extension setelah `url.Parse()`
- **Snapinsta href/data-url path check**: path extension divalidasi via `path.Ext()` — tolak URL kalo path-nya bukan `.mp4` (walau query param ada `.mp4`)
- **Frontend UI & download overlay**: ETA, speed, progress animation. Tersangkut di halaman sebelumnya — ini commit terpisah
- **Thumbnail Instagram kosong**: downloadgram HTML tidak punya `<meta og:image>`, hanya `<img alt="Thumb">` dengan token URL. Fix: extract dari `<img alt="Thumb">`, lalu decode JWT token untuk ambil direct CDN URL — thumbnail di-fetch langsung dari Instagram CDN, bukan via downloadgram proxy
- **Durasi video "0:00"**: Instagram scraper tidak extract duration. Fix: tambah `probeVideoDuration()` yang panggil `ffprobe` pada video URL untuk baca MP4 header dan extract durasi sebenarnya
- **Preview pakai video URL**: saat thumbnail kosong, preview fallback ke `option.url` (downloadgram video token) → `<img>` gagal render video. Fix: preview hanya tampil kalau thumbnail tersedia; kalo error/gak ada, tunjukkan "Preview tidak tersedia"
- **Download overlay belang**: `backdrop-blur-sm` di overlay + gradient background menimbulkan rendering artifact bergaris (belang). Fix: ganti jadi download bar fixed bottom tanpa overlay, solid colors
- **Durasi TikTok "0:00"**: `fetchTikTokVideos` hardcode `"0:00"` padahal TikWM API punya field `Duration`. Fix: parse `data.Duration`, format `m:ss`
- **Durasi Facebook kosong**: `fetchFacebookVideos` tidak set duration. Fix: tambah `probeVideoDuration()` fallback via ffprobe

## Instagram Video Engine
- **Method 1: Downloadgram** — POST ke `api.downloadgram.org/media`, parse HTML untuk:
  - Token JWT `cdn.downloadgram.org/?token=...` → decode JWT payload, cek filename `.mp4` via `isDownloadgramVideoToken()`
  - Direct CDN URLs → validasi path extension `.mp4` via `path.Ext()`
- **Method 2: Snapinsta** — scrape `snapinsta.app`:
  - `href` attribute link → validasi path extension `.mp4`
  - `data-url` attribute → validasi path extension `.mp4` atau `/video/` / `video_dashinit`
- **Method 3: Embed** — scrape `/reel/{shortcode}/embed/`, pakai `og:video:secure_url`
- **Final filter**: `filterVideoOptions()` selalu cek URL path extension, skip image, dedup

## Security (7 Layer Anti-DDoS)
Backend dilindungi 7 layer security di `backend/security.go` + middleware chain.

| Layer | Name | Implementation |
|---|---|---|
| 1 | **Rate Limiting** | Token bucket per IP. API: 30 req/s burst 60. Proxy: 10 req/s burst 20. 429 Too Many Requests. |
| 2 | **SSRF Protection** | Blokir 11 private IP ranges (localhost, 10.x, 192.168, 172.16-31, 169.254, dll). DNS lookup + CIDR match. |
| 3 | **Input Validation** | Cek `;`, `\|`, `` ` ``, `$()`, `${`, `&&`, `\|\|`. Batasi URL 2048 chars. Validasi scheme http/https. |
| 4 | **Security Headers** | X-Content-Type-Options: nosniff, X-Frame-Options: DENY, X-XSS-Protection, Referrer-Policy, Permissions-Policy. |
| 5 | **Panic Recovery** | Tangkap panic → log → return 500. Server tidak crash. |
| 6 | **Request Logging** | Log `[IP] METHOD /path query (duration)` untuk audit. |
| 7 | **Middleware Chain** | `logging → recovery → rateLimit → securityHeaders → cors → mux`. Timeout: Read 15s, Header 10s. |

Test: `go test -run TestRateLimiter\|TestSSRF\|TestInput\|TestSecurityHeaders\|TestPanic\|TestProxyHandler -v`

Simulasi: `bash test-security.sh http://localhost:8081` (200 concurrent flood + injection + SSRF + scheme validation)

## Download flow
1. User paste URL → klik Download
2. SSE ke `/api/stream/video?url=...` → backend cari opsi
3. User pilih kualitas → klik Unduh
4. Fetch `/api/proxy?url=...&yt_format=...` → backend pipe yt-dlp stdout ke response
