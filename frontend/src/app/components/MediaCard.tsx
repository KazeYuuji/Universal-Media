"use client";

import { FaDownload, FaEye } from "react-icons/fa";
import { isImageFormat, proxyUrl } from "../api";
import type { MediaOption } from "../api";

interface MediaCardProps {
  option: MediaOption;
  index: number;
  isSelected: boolean;
  mediaType: "video" | "image";
  thumbnail: string;
  onSelect: () => void;
  onDownload: () => void;
}

export default function MediaCard({
  option,
  index,
  isSelected,
  mediaType,
  thumbnail,
  onSelect,
  onDownload,
}: MediaCardProps) {
  const isImage = isImageFormat(option.format) || mediaType === "image";

  const getThumbnailUrl = () => {
    if (isImage) return option.url;
    const params = new URLSearchParams({ url: thumbnail || option.url, preview: "1" });
    return `/api/proxy?${params.toString()}`;
  };

  return (
    <div
      className={`rounded-2xl border overflow-hidden bg-white/50 dark:bg-zinc-950/40 transition-all ${
        isSelected
          ? mediaType === "image"
            ? "border-emerald-500 ring-2 ring-emerald-500/30"
            : "border-primary ring-2 ring-primary/30"
          : "border-gray-200 dark:border-zinc-800"
      }`}
    >
      <button
        type="button"
        onClick={onSelect}
        className={`w-full ${isImage ? "aspect-square" : "aspect-video"} bg-black relative block`}
        aria-label={`Lihat preview ${option.quality}`}
      >
        <img
          src={isImage ? proxyUrl(option.url, true) : (thumbnail ? proxyUrl(thumbnail, true) : "")}
          alt={option.quality}
          className="w-full h-full object-cover"
          loading="lazy"
          onError={(e) => {
            const img = e.currentTarget as HTMLImageElement;
            if (thumbnail) img.src = proxyUrl(thumbnail, true);
          }}
        />
        <span className="absolute top-2 left-2 bg-black/60 text-white text-[10px] font-bold px-2 py-0.5 rounded-md">
          #{index + 1}
        </span>
      </button>
      <div className="p-3 space-y-2">
        <p className="text-xs font-bold text-gray-900 dark:text-gray-100 line-clamp-2">{option.quality}</p>
        <div className="flex gap-2 items-center">
          <span className="text-[10px] uppercase font-extrabold px-2 py-0.5 rounded bg-gray-200 dark:bg-zinc-800 text-gray-700 dark:text-gray-300">
            {option.format}
          </span>
          <span className="text-[10px] text-gray-500 font-mono">{option.size}</span>
        </div>
        <div className="flex gap-2">
          {!isImage && (
            <button
              type="button"
              onClick={() => window.open(getThumbnailUrl(), "_blank", "noopener")}
              className="flex-1 py-2 rounded-lg text-xs font-semibold flex items-center justify-center gap-1 bg-primary/10 text-primary hover:bg-primary/20"
            >
              <FaEye /> Preview
            </button>
          )}
          <button
            type="button"
            onClick={onDownload}
            className={`flex-1 py-2 rounded-lg text-xs font-semibold text-white flex items-center justify-center gap-1 ${
              mediaType === "image"
                ? "bg-emerald-600 hover:bg-emerald-700"
                : "bg-primary hover:bg-primaryHover"
            }`}
          >
            <FaDownload /> Unduh
          </button>
        </div>
      </div>
    </div>
  );
}
