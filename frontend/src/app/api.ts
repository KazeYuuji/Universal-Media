export interface MediaOption {
  quality: string;
  format: string;
  size: string;
  url: string;
  audioUrl?: string;
  ytFormat?: string;
  directUrl?: string;
}

export interface MediaMetadata {
  mediaType: "video" | "image";
  title: string;
  duration: string;
  thumbnail: string;
  options: MediaOption[];
}

export const isImageFormat = (format: string) =>
  ["jpg", "jpeg", "png", "webp", "gif"].includes(format.toLowerCase());

export const isVideoFormat = (format: string) =>
  ["mp4", "webm", "mkv", "mov"].includes(format.toLowerCase());

export const BACKEND = process.env.NEXT_PUBLIC_BACKEND_URL || "http://localhost:8081";

export const proxyUrl = (rawUrl: string, preview = false, ytFormat?: string) => {
  let url = `${BACKEND}/api/proxy?url=${encodeURIComponent(rawUrl)}`;
  if (preview) url += "&preview=1";
  if (ytFormat) url += `&yt_format=${encodeURIComponent(ytFormat)}`;
  return url;
};

export const downloadUrl = (option: MediaOption, format?: string) => {
  const params = new URLSearchParams({ url: option.url });
  if (format === "mp3" || option.format === "mp3") params.set("convert", "mp3");
  if (option.ytFormat) params.set("yt_format", option.ytFormat);
  return `${BACKEND}/api/proxy?${params.toString()}`;
};

export const getPlatformFromUrl = (targetUrl: string) => {
  if (targetUrl.includes("youtube.com") || targetUrl.includes("youtu.be")) return "YouTube";
  if (targetUrl.includes("instagram.com")) return "Instagram";
  if (targetUrl.includes("tiktok.com") || targetUrl.includes("vm.tiktok.com") || targetUrl.includes("vt.tiktok.com")) return "TikTok";
  if (targetUrl.includes("facebook.com") || targetUrl.includes("fb.watch") || targetUrl.includes("fb.com")) return "Facebook";
  return "Unknown";
};
