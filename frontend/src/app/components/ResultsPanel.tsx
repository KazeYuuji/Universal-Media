"use client";

import { useState } from "react";
import { FaEye } from "react-icons/fa";
import { MediaMetadata, MediaOption, isImageFormat, proxyUrl } from "../api";
import MediaCard from "./MediaCard";

interface ResultsPanelProps {
  metadata: MediaMetadata;
  onDownload: (option: MediaOption) => void;
}

export default function ResultsPanel({ metadata, onDownload }: ResultsPanelProps) {
  const [selectedPreviewIndex, setSelectedPreviewIndex] = useState(0);
  const [previewError, setPreviewError] = useState(false);

  const items = getDisplayOptions(metadata);
  const selected = items[selectedPreviewIndex] ?? items[0];
  const isImage = metadata.mediaType === "image" || isImageFormat(selected.format);
  const hasThumbnail = !!(metadata.thumbnail);
  const previewSrc = metadata.mediaType === "image"
    ? proxyUrl(selected.url, true)
    : proxyUrl(metadata.thumbnail, true);

  const renderPreview = () => {
    if (previewError || !hasThumbnail) {
      return (
        <div className="w-full h-full flex items-center justify-center text-gray-400 dark:text-gray-600 text-sm px-4 text-center">
          <span>Preview tidak tersedia</span>
        </div>
      );
    }
    return (
      <img
        src={previewSrc}
        alt={selected.quality}
        className="w-full h-full object-contain max-h-[70vh]"
        loading="lazy"
        onError={() => setPreviewError(true)}
      />
    );
  };

  return (
    <div className="w-full bg-white/80 dark:bg-zinc-900/80 backdrop-blur-xl rounded-3xl p-6 md:p-8 shadow-2xl border border-white/30 dark:border-zinc-800/80 animate-fade-in-up space-y-6">
      <div>
        <span className={`inline-block text-xs px-3 py-1 rounded-full font-semibold uppercase tracking-wider mb-3 ${
          metadata.mediaType === "image"
            ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
            : "bg-primary/10 text-primary dark:bg-primary/20"
        }`}>
          {items.length} {metadata.mediaType === "image" ? "Foto" : "Video"} Ditemukan
        </span>
        <h3 className="text-xl md:text-2xl font-serif font-bold text-gray-900 dark:text-white leading-snug">
          {metadata.title}
        </h3>
        {metadata.mediaType === "video" && metadata.duration && (
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1 font-mono">Durasi: {metadata.duration}</p>
        )}
      </div>

      {hasThumbnail && (
        <div className="space-y-2">
          <p className="text-sm font-semibold text-gray-500 dark:text-gray-400 flex items-center gap-2">
            <FaEye className={metadata.mediaType === "image" ? "text-emerald-500" : "text-primary"} />
            Preview — {selected.quality}
          </p>
          <div className={`overflow-hidden rounded-2xl border border-gray-200 dark:border-zinc-800 bg-black flex items-center justify-center ${
            isImage ? "aspect-square max-h-[70vh]" : "aspect-video max-h-[70vh]"
          }`}>
            {renderPreview()}
          </div>
        </div>
      )}

      <div className="space-y-3">
        <p className="text-sm font-semibold text-gray-500 dark:text-gray-400">
          Semua {metadata.mediaType === "image" ? "Foto" : "Video"} ({items.length})
        </p>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {items.map((option, idx) => (
            <MediaCard
              key={`${option.url}-${idx}`}
              option={option}
              index={idx}
              isSelected={selectedPreviewIndex === idx}
              mediaType={metadata.mediaType}
              thumbnail={metadata.thumbnail}
              onSelect={() => { setSelectedPreviewIndex(idx); setPreviewError(false); }}
              onDownload={() => onDownload(option)}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

function getDisplayOptions(metadata: MediaMetadata) {
  if (!metadata) return [];
  if (metadata.mediaType === "image") {
    return metadata.options.filter((opt) => isImageFormat(opt.format));
  }
  return metadata.options.filter((opt) => !isImageFormat(opt.format));
}
