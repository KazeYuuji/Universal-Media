import { NextRequest, NextResponse } from 'next/server';

interface Format {
  itag: number;
  url?: string;
  mimeType: string;
  quality?: string;
  contentLength?: string;
  width?: number;
  height?: number;
  bitrate?: number;
  fps?: number;
}

interface PlayerResponse {
  streamingData?: {
    formats: Format[];
    adaptiveFormats: Format[];
    expiresInSeconds: string;
  };
  videoDetails?: {
    title: string;
    lengthSeconds: string;
    thumbnail?: { thumbnails: { url: string }[] };
  };
  playabilityStatus?: {
    status: string;
    reason?: string;
  };
  error?: { message: string };
}

const CLIENTS = [
  { name: 'ANDROID', version: '19.09.37', key: 'AIzaSyA8eiZmM1FaDVjRy-df2KTyQ_vz_yYM39w', ua: 'com.google.android.youtube/19.09.37 (Linux; U; Android 11) gzip', clientId: '3' },
  { name: 'ANDROID', version: '19.45.36', key: 'AIzaSyA8eiZmM1FaDVjRy-df2KTyQ_vz_yYM39w', ua: 'com.google.android.youtube/19.45.36 (Linux; U; Android 14) gzip', clientId: '3' },
  { name: 'WEB', version: '2.20250501.00.00', key: 'AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36', clientId: '1' },
  { name: 'WEB_CREATOR', version: '1.20250501.00.00', key: 'AIzaSyC8j1CJ6BQ0eDBiWLRhE2T3jUqW9Y8k9vM', ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36', clientId: '62' },
  { name: 'IOS', version: '19.45.36', key: 'AIzaSyAOghZGza2MQSZkY_zO0rT2hzLQ7JjF0e8', ua: 'com.google.ios.youtube/19.45.36 (iPhone; U; CPU iOS 17_5)', clientId: '5' },
];

async function callInnerTube(videoId: string): Promise<PlayerResponse | null> {
  for (const c of CLIENTS) {
    try {
      const payload = {
        videoId,
        context: {
          client: {
            clientName: c.name,
            clientVersion: c.version,
            hl: 'en',
            gl: 'US',
            clientScreen: c.clientId === '3' ? 'WATCH' : 'WATCH',
            androidSdkVersion: c.name === 'ANDROID' ? 34 : undefined,
          },
        },
        playbackContext: {
          contentPlaybackContext: {
            vis: 0,
            splay: false,
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
          'Origin': 'https://www.youtube.com',
        },
        body: JSON.stringify(payload),
        signal: AbortSignal.timeout(15000),
      });

      if (!res.ok) continue;
      const data: PlayerResponse = await res.json();
      if (data.error) continue;

      const ps = data.playabilityStatus;
      if (ps && ps.status !== 'OK') continue;
      if (!data.streamingData) continue;
      if (!data.streamingData.formats?.length && !data.streamingData.adaptiveFormats?.length) continue;

      return data;
    } catch {
      continue;
    }
  }
  return null;
}

async function fetchFromPage(videoId: string): Promise<PlayerResponse | null> {
  try {
    const res = await fetch(`https://www.youtube.com/watch?v=${videoId}`, {
      headers: {
        'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
        'Accept-Language': 'en-US,en;q=0.9',
      },
      signal: AbortSignal.timeout(10000),
    });
    if (!res.ok) return null;
    const html = await res.text();

    const patterns = [
      /ytInitialPlayerResponse\s*=\s*({.+?});/,
      /window\.ytInitialPlayerResponse\s*=\s*({.+?});/,
    ];

    for (const p of patterns) {
      const m = html.match(p);
      if (m) {
        try {
          const data: PlayerResponse = JSON.parse(m[1]);
          if (data.streamingData?.formats?.length || data.streamingData?.adaptiveFormats?.length) {
            return data;
          }
        } catch {}
      }
    }
  } catch {}
  return null;
}

export async function GET(request: NextRequest) {
  const videoId = request.nextUrl.searchParams.get('id');
  if (!videoId || !/^[a-zA-Z0-9_-]{11}$/.test(videoId)) {
    return NextResponse.json({ error: 'Invalid video ID' }, { status: 400 });
  }

  const data = await callInnerTube(videoId) || await fetchFromPage(videoId);

  if (!data) {
    return NextResponse.json({ error: 'No formats found' }, { status: 404 });
  }

  const title = data.videoDetails?.title || 'YouTube Video';
  const thumbs = data.videoDetails?.thumbnail?.thumbnails;
  const thumbnail = thumbs?.length ? thumbs[thumbs.length - 1].url : `https://img.youtube.com/vi/${videoId}/maxresdefault.jpg`;

  const seen = new Set<string>();
  const options: any[] = [];
  const allFormats = [...(data.streamingData?.formats || []), ...(data.streamingData?.adaptiveFormats || [])];

  for (const f of allFormats) {
    if (!f.url || seen.has(f.url)) continue;
    seen.add(f.url);

    const isAudio = f.mimeType?.includes('audio');
    const isVideo = f.mimeType?.includes('video');
    if (!isAudio && !isVideo) continue;

    let size = 'Dinamis';
    if (f.contentLength) {
      const bytes = parseInt(f.contentLength);
      if (!isNaN(bytes) && bytes > 0) {
        size = bytes >= 1073741824
          ? `~${(bytes / 1073741824).toFixed(1)} GB`
          : bytes >= 1048576
            ? `~${(bytes / 1048576).toFixed(1)} MB`
            : `~${(bytes / 1024).toFixed(0)} KB`;
      }
    }

    options.push({
      quality: isAudio ? 'Audio Only' : f.height ? `Video ${f.height}p` : f.quality || 'Video',
      format: isAudio ? 'm4a' : 'mp4',
      size,
      url: f.url,
      directUrl: f.url,
    });
  }

  if (options.length === 0) {
    return NextResponse.json({ error: 'No formats found' }, { status: 404 });
  }

  return NextResponse.json({ title, duration: '0:00', thumbnail, options, source: 'innertube' });
}
