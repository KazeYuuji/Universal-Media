import { NextRequest, NextResponse } from 'next/server';

const CLIENTS = [
  { name: 'TVHTML5', version: '7.20250501.00.00', key: 'AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8', ua: 'Mozilla/5.0 (Chromium; Linux x86_64) AppleWebKit/537.36' },
  { name: 'TVHTML5_SIMPLY', version: '7.20250501.00.00', key: 'AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8', ua: 'Mozilla/5.0 (Chromium; Linux x86_64) AppleWebKit/537.36' },
  { name: 'WEB', version: '2.20250501.00.00', key: 'AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36' },
];

async function getCookies(): Promise<string> {
  try {
    const res = await fetch('https://www.youtube.com', {
      headers: { 'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36' },
      signal: AbortSignal.timeout(8000),
    });
    const setCookie = res.headers.get('set-cookie') || '';
    return setCookie.split(',').map((c: string) => c.split(';')[0]).filter(Boolean).join('; ');
  } catch { return ''; }
}

export async function GET(request: NextRequest) {
  const videoId = request.nextUrl.searchParams.get('id');
  if (!videoId || !/^[a-zA-Z0-9_-]{11}$/.test(videoId)) {
    return NextResponse.json({ error: 'Invalid video ID' }, { status: 400 });
  }

  const cookies = await getCookies();

  for (const c of CLIENTS) {
    try {
      const payload = {
        videoId,
        context: {
          client: { clientName: c.name, clientVersion: c.version, hl: 'en', gl: 'US' },
        },
      };

      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        'User-Agent': c.ua,
        'Accept': '*/*',
        'Accept-Language': 'en-US,en;q=0.9',
      };
      if (cookies) headers['Cookie'] = cookies;

      const res = await fetch(`https://www.youtube.com/youtubei/v1/player?key=${c.key}`, {
        method: 'POST',
        headers,
        body: JSON.stringify(payload),
        signal: AbortSignal.timeout(15000),
      });

      if (!res.ok) continue;
      const data = await res.json();
      if (data.error) continue;
      if (data.playabilityStatus && data.playabilityStatus.status !== 'OK') continue;
      if (!data.streamingData?.formats?.length && !data.streamingData?.adaptiveFormats?.length) continue;

      const title = data.videoDetails?.title || 'YouTube Video';
      const thumbs = data.videoDetails?.thumbnail?.thumbnails;
      const thumbnail = thumbs?.length ? thumbs[thumbs.length - 1].url : `https://img.youtube.com/vi/${videoId}/maxresdefault.jpg`;

      const seen = new Set<string>();
      const options: any[] = [];
      for (const f of [...(data.streamingData.formats || []), ...(data.streamingData.adaptiveFormats || [])]) {
        if (!f.url || seen.has(f.url)) continue;
        seen.add(f.url);
        const isAudio = f.mimeType?.includes('audio');
        let size = 'Dinamis';
        if (f.contentLength) {
          const bytes = parseInt(f.contentLength);
          if (!isNaN(bytes) && bytes > 0) {
            size = bytes >= 1073741824 ? `~${(bytes / 1073741824).toFixed(1)} GB` : bytes >= 1048576 ? `~${(bytes / 1048576).toFixed(1)} MB` : `~${(bytes / 1024).toFixed(0)} KB`;
          }
        }
        options.push({ quality: isAudio ? 'Audio Only' : f.height ? `Video ${f.height}p` : f.quality || 'Video', format: isAudio ? 'm4a' : 'mp4', size, url: f.url, directUrl: f.url });
      }
      if (options.length > 0) {
        return NextResponse.json({ title, duration: '0:00', thumbnail, options, source: c.name });
      }
    } catch {}
  }

  // Try page scrape as last resort
  try {
    const res = await fetch(`https://www.youtube.com/watch?v=${videoId}`, {
      headers: {
        'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
        ...(cookies ? { 'Cookie': cookies } : {}),
      },
      signal: AbortSignal.timeout(10000),
    });
    if (res.ok) {
      const html = await res.text();
      const m = html.match(/ytInitialPlayerResponse\s*=\s*({.+?});/);
      if (m) {
        const data = JSON.parse(m[1]);
        if (data.playabilityStatus?.status === 'OK' && data.streamingData?.formats?.length) {
          const title = data.videoDetails?.title || 'YouTube Video';
          const thumbs = data.videoDetails?.thumbnail?.thumbnails;
          const thumbnail = thumbs?.length ? thumbs[thumbs.length - 1].url : `https://img.youtube.com/vi/${videoId}/maxresdefault.jpg`;
          const options: any[] = [];
          for (const f of [...(data.streamingData.formats || []), ...(data.streamingData.adaptiveFormats || [])]) {
            if (!f.url) continue;
            const isAudio = f.mimeType?.includes('audio');
            let size = 'Dinamis';
            if (f.contentLength) {
              const bytes = parseInt(f.contentLength);
              if (!isNaN(bytes) && bytes > 0) {
                size = bytes >= 1073741824 ? `~${(bytes / 1073741824).toFixed(1)} GB` : bytes >= 1048576 ? `~${(bytes / 1048576).toFixed(1)} MB` : `~${(bytes / 1024).toFixed(0)} KB`;
              }
            }
            options.push({ quality: isAudio ? 'Audio Only' : f.height ? `Video ${f.height}p` : f.quality || 'Video', format: isAudio ? 'm4a' : 'mp4', size, url: f.url, directUrl: f.url });
          }
          if (options.length > 0) return NextResponse.json({ title, duration: '0:00', thumbnail, options, source: 'page' });
        }
      }
    }
  } catch {}

  return NextResponse.json({ error: 'No formats found' }, { status: 404 });
}
