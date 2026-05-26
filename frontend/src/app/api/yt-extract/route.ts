import { NextRequest, NextResponse } from 'next/server';

const INNERTUBE_KEY = 'AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8';

interface YtFormat {
  itag: number;
  url?: string;
  mimeType: string;
  quality?: string;
  contentLength?: string;
  bitrate?: number;
  width?: number;
  height?: number;
  fps?: number;
}

interface YtStreamingData {
  formats: YtFormat[];
  adaptiveFormats: YtFormat[];
  expiresInSeconds: string;
}

interface YtPlayerResponse {
  streamingData?: YtStreamingData;
  videoDetails?: {
    title: string;
    lengthSeconds: string;
    thumbnail?: { thumbnails: { url: string }[] };
  };
  error?: { message: string };
}

async function callInnerTube(videoId: string, clientName: string, clientVersion: string): Promise<YtPlayerResponse | null> {
  const payload = {
    videoId,
    context: {
      client: {
        clientName,
        clientVersion,
        hl: 'en',
        gl: 'US',
      },
    },
  };

  try {
    const res = await fetch(`https://www.youtube.com/youtubei/v1/player?key=${INNERTUBE_KEY}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
      },
      body: JSON.stringify(payload),
      signal: AbortSignal.timeout(15000),
    });
    if (!res.ok) return null;
    const data: YtPlayerResponse = await res.json();
    if (data.error) return null;
    if (!data.streamingData || (!data.streamingData.formats?.length && !data.streamingData.adaptiveFormats?.length)) return null;
    return data;
  } catch {
    return null;
  }
}

export async function GET(request: NextRequest) {
  const videoId = request.nextUrl.searchParams.get('id');
  if (!videoId || !/^[a-zA-Z0-9_-]{11}$/.test(videoId)) {
    return NextResponse.json({ error: 'Invalid video ID' }, { status: 400 });
  }

  interface Option {
    quality: string;
    format: string;
    size: string;
    url: string;
    ytFormat?: string;
    directUrl?: string;
  }

  const options: Option[] = [];
  let title = 'YouTube Video';
  let thumbnail = `https://img.youtube.com/vi/${videoId}/maxresdefault.jpg`;

  const clients = [
    { name: 'WEB', version: '2.20240101.00.00' },
    { name: 'ANDROID', version: '19.09.37' },
    { name: 'WEB', version: '2.20241101.00.00' },
  ];

  for (const client of clients) {
    const data = await callInnerTube(videoId, client.name, client.version);
    if (!data) continue;

    if (data.videoDetails) {
      title = data.videoDetails.title || title;
      const thumbs = data.videoDetails.thumbnail?.thumbnails;
      if (thumbs?.length) thumbnail = thumbs[thumbs.length - 1].url;
    }

    const seen = new Set<string>();
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

      const opt: Option = {
        quality: isAudio ? 'Audio Only' : f.height ? `Video ${f.height}p` : f.quality || 'Video',
        format: isAudio ? 'm4a' : 'mp4',
        size,
        url: f.url,
        directUrl: f.url,
      };
      options.push(opt);
    }

    if (options.length > 0) break;
  }

  if (options.length === 0) {
    return NextResponse.json({ error: 'No formats found' }, { status: 404 });
  }

  return NextResponse.json({
    title,
    duration: '0:00',
    thumbnail,
    options,
    source: 'innertube',
  });
}
