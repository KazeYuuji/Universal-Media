import { NextRequest, NextResponse } from 'next/server';

async function tryPipedAPI(videoId: string) {
  const instances = [
    'https://pipedapi.kavin.rocks',
    'https://pipedapi.smnz.de',
    'https://pipedapi.r4fo.com',
    'https://pipedapi.tiekoetter.com',
  ];
  for (const base of instances) {
    try {
      const res = await fetch(`${base}/streams/${videoId}`, { signal: AbortSignal.timeout(8000) });
      if (!res.ok) continue;
      const data = await res.json();
      if (data.error) continue;
      const streams: any[] = [];
      for (const v of data.videoStreams || []) {
        if (v.url) streams.push({ quality: v.quality || 'Video', format: 'mp4', size: 'Dinamis', url: v.url, directUrl: v.url });
      }
      for (const a of data.audioStreams || []) {
        if (a.url) streams.push({ quality: 'Audio Only', format: 'm4a', size: 'Dinamis', url: a.url, directUrl: a.url });
      }
      if (streams.length > 0) {
        return { title: data.title || 'YouTube Video', duration: data.duration || '0:00', thumbnail: data.thumbnailUrl || `https://img.youtube.com/vi/${videoId}/maxresdefault.jpg`, options: streams, source: 'piped' };
      }
    } catch {}
  }
  return null;
}

async function tryCobaltAPI(videoId: string) {
  const instances = ['https://co.wuk.sh', 'https://cobalt.tronicsdev.com'];
  for (const base of instances) {
    try {
      const res = await fetch(`${base}/api/json`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
        body: JSON.stringify({
          url: `https://youtube.com/watch?v=${videoId}`,
          downloadMode: 'auto',
          filenameStyle: 'basic',
          disableMetadata: false,
        }),
        signal: AbortSignal.timeout(10000),
      });
      if (!res.ok) continue;
      const data = await res.json();
      if (data.url) {
        return {
          title: data.title || 'YouTube Video',
          duration: '0:00',
          thumbnail: `https://img.youtube.com/vi/${videoId}/maxresdefault.jpg`,
          options: [{
            quality: 'Video',
            format: data.filename?.endsWith('.mp3') ? 'mp3' : 'mp4',
            size: `~${((data.size || 0) / 1048576).toFixed(1)} MB`,
            url: data.url,
            directUrl: data.url,
          }],
          source: 'cobalt',
        };
      }
    } catch {}
  }
  return null;
}

export async function GET(request: NextRequest) {
  const videoId = request.nextUrl.searchParams.get('id');
  if (!videoId || !/^[a-zA-Z0-9_-]{11}$/.test(videoId)) {
    return NextResponse.json({ error: 'Invalid video ID' }, { status: 400 });
  }

  const piped = await tryPipedAPI(videoId);
  if (piped) return NextResponse.json(piped);

  const cobalt = await tryCobaltAPI(videoId);
  if (cobalt) return NextResponse.json(cobalt);

  return NextResponse.json({ error: 'No formats found' }, { status: 404 });
}
