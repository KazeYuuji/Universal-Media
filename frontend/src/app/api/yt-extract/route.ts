import { NextRequest, NextResponse } from 'next/server';

const CLIENTS = [
  { name: 'TVHTML5', version: '7.20250501.00.00', key: 'AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8', ua: 'Mozilla/5.0 (Chromium; Linux x86_64) AppleWebKit/537.36' },
  { name: 'TVHTML5_SIMPLY', version: '7.20250501.00.00', key: 'AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8', ua: 'Mozilla/5.0 (Chromium; Linux x86_64) AppleWebKit/537.36' },
  { name: 'WEB', version: '2.20250501.00.00', key: 'AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36' },
  { name: 'ANDROID', version: '19.45.36', key: 'AIzaSyB-63vPrJDK6oUE94QhCn5UDkQvPAMqP-I', ua: 'com.google.android.youtube/19.45.36 (Linux; U; Android 14) gzip' },
];

async function getCookies(): Promise<string> {
  try {
    const res = await fetch('https://www.youtube.com', {
      headers: { 'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36' },
      signal: AbortSignal.timeout(8000),
    });
    return res.headers.get('set-cookie')?.split(',').map((c: string) => c.split(';')[0]).filter(Boolean).join('; ') || '';
  } catch { return ''; }
}

export async function GET(request: NextRequest) {
  const videoId = request.nextUrl.searchParams.get('id');
  const debug = request.nextUrl.searchParams.has('debug');

  if (!videoId || !/^[a-zA-Z0-9_-]{11}$/.test(videoId)) {
    return NextResponse.json({ error: 'Invalid video ID' }, { status: 400 });
  }

  const cookies = await getCookies();
  const logs: any[] = [];

  for (const c of CLIENTS) {
    const log: any = { client: `${c.name} v${c.version}` };
    try {
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        'User-Agent': c.ua,
      };
      if (cookies) headers['Cookie'] = cookies;

      const res = await fetch(`https://www.youtube.com/youtubei/v1/player?key=${c.key}`, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          videoId,
          context: { client: { clientName: c.name, clientVersion: c.version, hl: 'en', gl: 'US' } },
        }),
        signal: AbortSignal.timeout(10000),
      });

      log.status = res.status;
      const txt = await res.text();
      log.preview = txt.slice(0, 200);
      try {
        const j = JSON.parse(txt);
        log.playability = j.playabilityStatus?.status;
        log.reason = j.playabilityStatus?.reason;
        log.fmt = j.streamingData?.formats?.length || 0;
        log.afmt = j.streamingData?.adaptiveFormats?.length || 0;
        if (res.ok && j.playabilityStatus?.status === 'OK' && (log.fmt + log.afmt) > 0) {
          if (!debug) {
            const title = j.videoDetails?.title || 'YouTube Video';
            const thumbs = j.videoDetails?.thumbnail?.thumbnails;
            const thumbnail = thumbs?.length ? thumbs[thumbs.length - 1].url : `https://img.youtube.com/vi/${videoId}/maxresdefault.jpg`;
            const options: any[] = [];
            const seen = new Set<string>();
            for (const f of [...(j.streamingData.formats || []), ...(j.streamingData.adaptiveFormats || [])]) {
              if (!f.url || seen.has(f.url)) continue;
              seen.add(f.url);
              const isAudio = f.mimeType?.includes('audio');
              let size = 'Dinamis';
              if (f.contentLength) { const b = parseInt(f.contentLength); if (!isNaN(b) && b > 0) size = b >= 1073741824 ? `~${(b / 1073741824).toFixed(1)} GB` : b >= 1048576 ? `~${(b / 1048576).toFixed(1)} MB` : `~${(b / 1024).toFixed(0)} KB`; }
              options.push({ quality: isAudio ? 'Audio Only' : f.height ? `Video ${f.height}p` : f.quality || 'Video', format: isAudio ? 'm4a' : 'mp4', size, url: f.url, directUrl: f.url });
            }
            return NextResponse.json({ title, duration: '0:00', thumbnail, options, source: c.name });
          }
        }
      } catch {}
    } catch (e: any) { log.error = e?.message; }
    logs.push(log);
  }

  // Page scrape
  try {
    const log: any = { client: 'page_scrape' };
    const res = await fetch(`https://www.youtube.com/watch?v=${videoId}`, {
      headers: { 'User-Agent': 'Mozilla/5.0', ...(cookies ? { 'Cookie': cookies } : {}) },
      signal: AbortSignal.timeout(10000),
    });
    log.status = res.status;
    const html = await res.text();
    log.htmlLen = html.length;
    const m = html.match(/ytInitialPlayerResponse\s*=\s*({.+?});/);
    if (m) {
      try {
        const j = JSON.parse(m[1]);
        log.playability = j.playabilityStatus?.status;
        log.reason = j.playabilityStatus?.reason;
        log.fmt = j.streamingData?.formats?.length || 0;
        log.afmt = j.streamingData?.adaptiveFormats?.length || 0;
        if (j.playabilityStatus?.status === 'OK' && (log.fmt + log.afmt) > 0) {
          if (!debug) {
            const title = j.videoDetails?.title || 'YouTube Video';
            const thumbs = j.videoDetails?.thumbnail?.thumbnails;
            const thumbnail = thumbs?.length ? thumbs[thumbs.length - 1].url : `https://img.youtube.com/vi/${videoId}/maxresdefault.jpg`;
            const options: any[] = [];
            for (const f of [...(j.streamingData.formats || []), ...(j.streamingData.adaptiveFormats || [])]) {
              if (!f.url) continue;
              const isAudio = f.mimeType?.includes('audio');
              let size = 'Dinamis';
              if (f.contentLength) { const b = parseInt(f.contentLength); if (!isNaN(b) && b > 0) size = b >= 1073741824 ? `~${(b / 1073741824).toFixed(1)} GB` : b >= 1048576 ? `~${(b / 1048576).toFixed(1)} MB` : `~${(b / 1024).toFixed(0)} KB`; }
              options.push({ quality: isAudio ? 'Audio Only' : f.height ? `Video ${f.height}p` : f.quality || 'Video', format: isAudio ? 'm4a' : 'mp4', size, url: f.url, directUrl: f.url });
            }
            return NextResponse.json({ title, duration: '0:00', thumbnail, options, source: 'page' });
          }
        }
      } catch {}
    }
    logs.push(log);
  } catch (e: any) { logs.push({ client: 'page_scrape', error: e?.message }); }

  if (debug) return NextResponse.json({ error: 'All failed', logs, hasCookies: !!cookies });
  return NextResponse.json({ error: 'No formats found' }, { status: 404 });
}
