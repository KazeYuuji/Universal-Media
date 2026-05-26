import { NextRequest, NextResponse } from 'next/server';

const CLIENTS = [
  { name: 'ANDROID', version: '19.09.37', key: 'AIzaSyA8eiZmM1FaDVjRy-df2KTyQ_vz_yYM39w', ua: 'com.google.android.youtube/19.09.37 (Linux; U; Android 11) gzip', clientId: '3' },
  { name: 'WEB', version: '2.20250501.00.00', key: 'AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36', clientId: '1' },
  { name: 'IOS', version: '19.45.36', key: 'AIzaSyAOghZGza2MQSZkY_zO0rT2hzLQ7JjF0e8', ua: 'com.google.ios.youtube/19.45.36 (iPhone; U; CPU iOS 17_5)', clientId: '5' },
  { name: 'WEB_CREATOR', version: '1.20250501.00.00', key: 'AIzaSyC8j1CJ6BQ0eDBiWLRhE2T3jUqW9Y8k9vM', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36', clientId: '62' },
  { name: 'ANDROID', version: '19.45.36', key: 'AIzaSyA8eiZmM1FaDVjRy-df2KTyQ_vz_yYM39w', ua: 'com.google.android.youtube/19.45.36 (Linux; U; Android 14) gzip', clientId: '3' },
];

export async function GET(request: NextRequest) {
  const videoId = request.nextUrl.searchParams.get('id');
  const debug = request.nextUrl.searchParams.has('debug');

  if (!videoId || !/^[a-zA-Z0-9_-]{11}$/.test(videoId)) {
    return NextResponse.json({ error: 'Invalid video ID' }, { status: 400 });
  }

  const logs: any[] = [];

  // Try InnerTube API
  for (const c of CLIENTS) {
    const log: any = { client: `${c.name} v${c.version}`, key: c.key.slice(0, 20) + '...' };
    try {
      const payload = {
        videoId,
        context: {
          client: {
            clientName: c.name,
            clientVersion: c.version,
            hl: 'en',
            gl: 'US',
          },
        },
      };

      const res = await fetch(`https://www.youtube.com/youtubei/v1/player?key=${c.key}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'User-Agent': c.ua,
          'X-YouTube-Client-Name': c.clientId,
          'X-YouTube-Client-Version': c.version,
        },
        body: JSON.stringify(payload),
        signal: AbortSignal.timeout(10000),
      });

      log.status = res.status;
      const text = await res.text();
      log.bodyPreview = text.slice(0, 300);

      try {
        const json = JSON.parse(text);
        log.playability = json.playabilityStatus?.status;
        log.playabilityReason = json.playabilityStatus?.reason;
        log.hasStreaming = !!json.streamingData;
        log.formatCount = json.streamingData?.formats?.length || 0;
        log.adaptiveCount = json.streamingData?.adaptiveFormats?.length || 0;
      } catch {}

      if (res.ok && log.formatCount + log.adaptiveCount > 0) {
        logs.push(log);
        if (!debug) {
          const data = JSON.parse(text);
          const title = data.videoDetails?.title || 'YouTube Video';
          const thumbs = data.videoDetails?.thumbnail?.thumbnails;
          const thumbnail = thumbs?.length ? thumbs[thumbs.length - 1].url : `https://img.youtube.com/vi/${videoId}/maxresdefault.jpg`;
          const seen = new Set<string>();
          const options: any[] = [];
          for (const f of [...(data.streamingData?.formats || []), ...(data.streamingData?.adaptiveFormats || [])]) {
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
          return NextResponse.json({ title, duration: '0:00', thumbnail, options, source: c.name });
        }
      }
    } catch (e: any) {
      log.error = e?.message?.slice(0, 100) || String(e);
    }
    logs.push(log);
  }

  // Try page scrape fallback
  try {
    const pageLog: any = { method: 'page_scrape' };
    const res = await fetch(`https://www.youtube.com/watch?v=${videoId}`, {
      headers: { 'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36' },
      signal: AbortSignal.timeout(10000),
    });
    pageLog.status = res.status;
    const html = await res.text();
    pageLog.htmlLen = html.length;
    const m = html.match(/ytInitialPlayerResponse\s*=\s*({.+?});/);
    pageLog.hasInitialData = !!m;
    if (m) {
      try {
        const data = JSON.parse(m[1]);
        pageLog.playability = data.playabilityStatus?.status;
        pageLog.formatCount = data.streamingData?.formats?.length || 0;
        pageLog.adaptiveCount = data.streamingData?.adaptiveFormats?.length || 0;
        if (data.streamingData?.formats?.length || data.streamingData?.adaptiveFormats?.length) {
          logs.push(pageLog);
          if (!debug) {
            const title = data.videoDetails?.title || 'YouTube Video';
            const thumbs = data.videoDetails?.thumbnail?.thumbnails;
            const thumbnail = thumbs?.length ? thumbs[thumbs.length - 1].url : `https://img.youtube.com/vi/${videoId}/maxresdefault.jpg`;
            const seen = new Set<string>();
            const options: any[] = [];
            for (const f of [...(data.streamingData?.formats || []), ...(data.streamingData?.adaptiveFormats || [])]) {
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
            return NextResponse.json({ title, duration: '0:00', thumbnail, options, source: 'page' });
          }
        }
      } catch {}
    }
    logs.push(pageLog);
  } catch (e: any) {
    logs.push({ method: 'page_scrape', error: e?.message?.slice(0, 100) });
  }

  if (debug) {
    return NextResponse.json({ error: 'All methods failed', logs });
  }
  return NextResponse.json({ error: 'No formats found' }, { status: 404 });
}
