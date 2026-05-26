"use client";

import { useState, useEffect, useRef } from "react";
import {
  FaInstagram, FaTiktok, FaFacebookF, FaYoutube,
  FaMoon, FaSun, FaDownload, FaSpinner,
  FaHistory, FaTrash, FaQuestionCircle,
  FaChevronDown, FaChevronUp, FaLink, FaImage, FaFilm,
} from "react-icons/fa";
import { MediaMetadata, MediaOption, proxyUrl, getPlatformFromUrl, BACKEND } from "./api";
import ResultsPanel from "./components/ResultsPanel";

interface HistoryItem {
  id: string;
  url: string;
  platform: string;
  title: string;
  thumbnail: string;
  timestamp: string;
}

export default function Home() {
  const [url, setUrl] = useState("");
  const [downloadMode, setDownloadMode] = useState<"video" | "image">("video");
  const [activeTab, setActiveTab] = useState("all");
  const [isDarkMode, setIsDarkMode] = useState(true);
  const [status, setStatus] = useState("");
  const [progress, setProgress] = useState(0);
  const [isProcessing, setIsProcessing] = useState(false);
  const [error, setError] = useState("");
  const [metadata, setMetadata] = useState<MediaMetadata | null>(null);
  const [history, setHistory] = useState<HistoryItem[]>([]);
  const [expandedFaq, setExpandedFaq] = useState<number | null>(null);
  const [isDownloading, setIsDownloading] = useState(false);
  const [downloadProgress, setDownloadProgress] = useState(0);
  const [downloadSpeed, setDownloadSpeed] = useState("");
  const [downloadEta, setDownloadEta] = useState("");
  const [downloadStatus, setDownloadStatus] = useState("");
  const downloadStart = useRef(0);
  const lastLoaded = useRef(0);

  useEffect(() => {
    if (typeof window !== "undefined") {
      const isDark = document.documentElement.classList.contains("dark");
      setIsDarkMode(isDark);
      const savedHistory = localStorage.getItem("download_history");
      if (savedHistory) {
        try { setHistory(JSON.parse(savedHistory)); } catch (_) {}
      }
    }
  }, []);

  const toggleTheme = () => {
    const root = document.documentElement;
    if (isDarkMode) {
      root.classList.remove("dark"); localStorage.theme = "light"; setIsDarkMode(false);
    } else {
      root.classList.add("dark"); localStorage.theme = "dark"; setIsDarkMode(true);
    }
  };

  const getPlaceholder = () => {
    if (downloadMode === "image") {
      switch (activeTab) {
        case "youtube": return "Tempel link YouTube untuk unduh thumbnail...";
        case "instagram": return "Tempel link post foto Instagram (/p/...)...";
        case "tiktok": return "Tempel link foto/slideshow TikTok...";
        case "facebook": return "Tempel link post foto Facebook...";
        default: return "Tempel link FOTO dari IG, TikTok, FB, atau thumbnail YouTube...";
      }
    }
    switch (activeTab) {
      case "youtube": return "Tempel link YouTube, Shorts, atau Audio...";
      case "instagram": return "Tempel link Reels atau Video IG...";
      case "tiktok": return "Tempel link video TikTok...";
      case "facebook": return "Tempel link video Facebook...";
      default: return "Tempel link VIDEO dari YouTube, IG, TikTok, atau Facebook...";
    }
  };

  const formatSize = (bytes: number) => {
    if (bytes <= 0) return "0 B";
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  const sanitizeFilename = (s: string) => s.replace(/[<>:"/\\|?*]/g, '_').trim() || 'download';

  const parseSize = (s: string): number | null => {
    const clean = s.replace(/^~/, '').trim();
    const m = clean.match(/^([\d.]+)\s*(KB|MB|GB)$/i);
    if (!m) return null;
    const v = parseFloat(m[1]);
    const u = m[2].toLowerCase();
    return u === 'gb' ? v * 1024 * 1024 * 1024 : u === 'mb' ? v * 1024 * 1024 : v * 1024;
  };

  const formatEta = (seconds: number) => {
    if (seconds < 0 || !isFinite(seconds)) return "";
    if (seconds < 60) return `${Math.ceil(seconds)}d`;
    const m = Math.floor(seconds / 60);
    const s = Math.ceil(seconds % 60);
    return `${m}m ${s}d`;
  };

  const triggerDirectDownload = (option: MediaOption, filename: string) => {
    const a = document.createElement("a");
    a.href = option.directUrl!;
    a.download = `${filename}.${option.format || "bin"}`;
    a.target = "_blank";
    a.rel = "noopener";
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    setIsDownloading(false);
    setProgress(100);
    setDownloadStatus("Mengunduh langsung dari CDN...");
    setTimeout(() => setDownloadStatus("Selesai"), 2000);
  };

  const triggerDownload = (option: MediaOption) => {
    setIsDownloading(true);
    setDownloadProgress(0);
    setDownloadSpeed("");
    setDownloadEta("");
    downloadStart.current = Date.now();
    lastLoaded.current = 0;

    const safeTitle = sanitizeFilename(metadata?.title || 'media');
    const qualityLabel = option.quality.replace(/\s*\(.*?\)\s*/g, '').trim();
    const filename = `${safeTitle} - ${qualityLabel}`;
    const totalBytes = parseSize(option.size);

    // Direct CDN URL from Vercel API — skip proxy, download straight from browser
    if (option.directUrl) {
      triggerDirectDownload(option, filename);
      return;
    }

    setDownloadStatus(`Mengunduh ${qualityLabel}...`);

    const params = new URLSearchParams({ url: option.url });
    if (option.format === "mp3") params.set("convert", "mp3");
    if (option.ytFormat) params.set("yt_format", option.ytFormat);
    params.set("filename", filename);

    const dlUrl = `${BACKEND}/api/proxy?${params.toString()}`;

    const xhr = new XMLHttpRequest();
    xhr.open("GET", dlUrl, true);
    xhr.responseType = "blob";

    xhr.onprogress = (e) => {
      let pct = 0;
      let label = "";
      const now = Date.now();
      const elapsed = (now - downloadStart.current) / 1000;
      const loaded = e.loaded;

      if (e.lengthComputable) {
        pct = Math.round((loaded / e.total) * 100);
        label = `${formatSize(loaded)}/${formatSize(e.total)}`;
      } else if (totalBytes) {
        const raw = Math.round((loaded / totalBytes) * 100);
        pct = Math.min(raw, 99);
        if (pct < 5 && loaded > 0) pct = Math.max(pct, 5);
        label = `${formatSize(loaded)}/${option.size}`;
      } else {
        pct = Math.min(Math.round((loaded / (1024 * 1024)) * 5), 95);
        label = formatSize(loaded);
      }

      if (elapsed > 1 && loaded > 0) {
        const speedBps = loaded / elapsed;
        setDownloadSpeed(`${formatSize(speedBps)}/d`);
        const remaining = totalBytes ? totalBytes - loaded : 0;
        if (speedBps > 0 && remaining > 0) {
          setDownloadEta(formatEta(remaining / speedBps));
        }
      }

      setDownloadProgress(pct);
      setDownloadStatus(`Mengunduh ${qualityLabel} (${label})`);
    };

    xhr.onload = () => {
      if (xhr.status === 200) {
        const blob = xhr.response;
        const blobUrl = URL.createObjectURL(blob);
        const actualSize = formatSize(blob.size);
        const totalTime = ((Date.now() - downloadStart.current) / 1000).toFixed(0);
        setDownloadProgress(100);
        setDownloadSpeed("");
        setDownloadEta("");
        setDownloadStatus(`Selesai — ${actualSize} (${totalTime}d)`);
        setTimeout(() => {
          setIsDownloading(false);
          const a = document.createElement("a");
          a.href = blobUrl;
          a.download = `${filename}.${option.format || "bin"}`;
          document.body.appendChild(a);
          a.click();
          document.body.removeChild(a);
          setTimeout(() => URL.revokeObjectURL(blobUrl), 10000);
        }, 800);
      } else {
        // Proxy failed — try direct download from source URL
        setIsDownloading(false);
        window.open(option.url, "_blank", "noopener");
      }
    };

    xhr.onerror = () => {
      setIsDownloading(false);
      window.open(option.url, "_blank", "noopener");
    };

    xhr.send();
  };

  const ytVideoId = (targetUrl: string): string | null => {
    const patterns = [
      /(?:youtube\.com\/watch\?v=|youtu\.be\/)([a-zA-Z0-9_-]{11})/,
      /youtube\.com\/embed\/([a-zA-Z0-9_-]{11})/,
      /youtube\.com\/shorts\/([a-zA-Z0-9_-]{11})/,
    ];
    for (const p of patterns) {
      const m = targetUrl.match(p);
      if (m) return m[1];
    }
    return null;
  };

  const startSse = (targetUrl: string) => {
    const streamPath = downloadMode === "image" ? "image" : "video";
    const evtSource = new EventSource(`${BACKEND}/api/stream/${streamPath}?url=${encodeURIComponent(targetUrl)}`);

    evtSource.onmessage = (event) => {
      const data = JSON.parse(event.data);
      if (data.error) {
        setError(data.error);
        setIsProcessing(false);
        evtSource.close();
        return;
      }

      setStatus(data.status);
      setProgress(data.progress);

      if (data.status === "Completed") {
        const meta: MediaMetadata = {
          mediaType: data.mediaType === "image" ? "image" : "video",
          title: data.title,
          duration: data.duration,
          thumbnail: data.thumbnail,
          options: data.options || [],
        };
        setMetadata(meta);
        setIsProcessing(false);
        evtSource.close();

        const newItem: HistoryItem = {
          id: Math.random().toString(36).substring(2, 9),
          url: targetUrl,
          platform: getPlatformFromUrl(targetUrl),
          title: data.title,
          thumbnail: data.thumbnail,
          timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
        };
        const updatedHistory = [newItem, ...history.slice(0, 4)];
        setHistory(updatedHistory);
        localStorage.setItem("download_history", JSON.stringify(updatedHistory));
      }
    };

    evtSource.onerror = () => {
      setError("Koneksi ke server terputus atau backend tidak aktif.");
      setIsProcessing(false);
      evtSource.close();
    };
  };

  const handleDownload = (e: React.FormEvent) => {
    e.preventDefault();
    if (!url) return;

    setIsProcessing(true);
    setProgress(0);
    setStatus(downloadMode === "image" ? "Menganalisis link foto..." : "Menganalisis link video...");
    setMetadata(null);
    setError("");

    const vid = ytVideoId(url);
    if (vid) {
      setStatus("Mengekstrak dari Vercel API...");
      fetch(`/api/yt-extract?id=${vid}`)
        .then((r) => {
          if (!r.ok) throw new Error("Vercel API gagal");
          return r.json();
        })
        .then((data) => {
          if (data.error || !data.options?.length) throw new Error(data.error || "Tidak ada format");
          const meta: MediaMetadata = {
            mediaType: "video",
            title: data.title,
            duration: data.duration || "0:00",
            thumbnail: data.thumbnail,
            options: data.options.map((o: any) => ({
              quality: o.quality,
              format: o.format,
              size: o.size,
              url: o.url,
              directUrl: o.directUrl || o.url,
              ytFormat: "direct",
            })),
          };
          setMetadata(meta);
          setIsProcessing(false);
          const newItem: HistoryItem = {
            id: Math.random().toString(36).substring(2, 9),
            url, platform: "YouTube", title: data.title, thumbnail: data.thumbnail,
            timestamp: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
          };
          const updatedHistory = [newItem, ...history.slice(0, 4)];
          setHistory(updatedHistory);
          localStorage.setItem("download_history", JSON.stringify(updatedHistory));
        })
        .catch(() => {
          setStatus("Vercel API gagal, fallback ke backend...");
          startSse(url);
        });
    } else {
      startSse(url);
    }
  };

  const clearHistory = () => { setHistory([]); localStorage.removeItem("download_history"); };
  const removeHistoryItem = (id: string) => {
    const updated = history.filter(item => item.id !== id);
    setHistory(updated);
    localStorage.setItem("download_history", JSON.stringify(updated));
  };

  const tabs = [
    { id: "all", label: "All-in-One", icon: <FaLink /> },
    { id: "youtube", label: "YouTube", icon: <FaYoutube />, activeColor: "bg-red-600 text-white" },
    { id: "instagram", label: "Instagram", icon: <FaInstagram />, activeColor: "bg-gradient-to-r from-yellow-500 via-pink-500 to-purple-600 text-white" },
    { id: "tiktok", label: "TikTok", icon: <FaTiktok />, activeColor: "bg-zinc-800 text-white dark:bg-white dark:text-black" },
    { id: "facebook", label: "Facebook", icon: <FaFacebookF />, activeColor: "bg-blue-600 text-white" }
  ];

  return (
    <div className="flex-grow flex flex-col items-center p-4 md:p-8 relative bg-gradient-to-b from-classicLight via-classicLight to-neutral-200 dark:from-classicDark dark:via-classicDark dark:to-neutral-900 min-h-screen">
      <div className="absolute top-[-10%] left-[-10%] w-[50%] h-[50%] bg-primary/10 rounded-full blur-[120px] pointer-events-none" />
      <div className="absolute bottom-[20%] right-[-10%] w-[40%] h-[40%] bg-primary/5 rounded-full blur-[100px] pointer-events-none" />

      <button onClick={toggleTheme} className="absolute top-6 right-6 p-3 rounded-full bg-white/40 hover:bg-white/70 dark:bg-white/5 dark:hover:bg-white/10 backdrop-blur-md border border-gray-200/50 dark:border-gray-800/50 shadow-md z-50 text-gray-800 dark:text-yellow-400" aria-label="Toggle Theme">
        {isDarkMode ? <FaSun className="text-xl" /> : <FaMoon className="text-xl" />}
      </button>

      <div className="max-w-4xl w-full flex flex-col items-center space-y-10 mt-10 z-10">
        {/* Hero */}
        <div className="text-center space-y-3">
          <div className="inline-flex items-center gap-2 px-4 py-1.5 rounded-full bg-primary/10 text-primary dark:text-primary border border-primary/20 text-sm font-semibold tracking-wide uppercase shadow-sm">
            Premium Downloader Hub
          </div>
          <h1 className="text-5xl md:text-7xl font-serif font-extrabold tracking-tight text-gray-900 dark:text-white drop-shadow-sm">
            Universal<span className="text-primary italic font-normal">Media</span>
          </h1>
          <p className="text-base md:text-xl text-gray-600 dark:text-gray-300 font-light max-w-xl mx-auto leading-relaxed">
            Unduh video, audio, reels, story, dan foto favorit dari 4 platform besar dalam resolusi terbaik secara instan.
          </p>
        </div>

        {/* Tabs */}
        <div className="flex flex-wrap justify-center gap-2 p-1.5 bg-white/50 dark:bg-white/5 backdrop-blur-md rounded-2xl border border-gray-200/60 dark:border-gray-800/40 w-full max-w-2xl shadow-lg">
          {tabs.map((tab) => (
            <button key={tab.id} onClick={() => { setActiveTab(tab.id); setError(""); }}
              className={`flex items-center gap-2 px-4 py-2.5 rounded-xl text-sm font-medium transition-all duration-300 ${
                activeTab === tab.id
                  ? tab.activeColor || "bg-primary text-white shadow-mdScale"
                  : "text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-zinc-800/50"
              }`}
            >
              {tab.icon} <span>{tab.label}</span>
            </button>
          ))}
        </div>

        {/* Input Card */}
        <div className="w-full max-w-3xl bg-white/70 dark:bg-zinc-900/60 backdrop-blur-xl rounded-3xl p-6 shadow-2xl border border-white/20 dark:border-zinc-800/60">
          <form onSubmit={handleDownload} className="flex flex-col gap-4">
            <div className="relative flex-grow w-full">
              <input type="url" required placeholder={getPlaceholder()} value={url} onChange={(e) => setUrl(e.target.value)}
                className="w-full bg-gray-100/50 dark:bg-zinc-950/40 p-4 md:p-5 rounded-2xl text-gray-900 dark:text-gray-100 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-primary/50 text-base md:text-lg border border-gray-200/50 dark:border-zinc-800/50"
                disabled={isProcessing}
              />
            </div>
            <div className="flex gap-2">
              <button type="submit" disabled={isProcessing || !url}
                className={`flex-1 text-white px-6 py-3.5 rounded-2xl font-semibold transition-all duration-300 flex items-center justify-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed shadow-lg ${
                  downloadMode === "image" ? "bg-emerald-600 hover:bg-emerald-700" : "bg-primary hover:bg-primaryHover"
                }`}
              >
                {isProcessing ? <FaSpinner className="animate-spin text-xl" /> : <FaDownload className="text-xl" />}
                <span>{isProcessing ? "Memproses..." : "Cari Media"}</span>
              </button>
              <div className="flex gap-1 p-0.5 bg-gray-100/80 dark:bg-zinc-950/60 rounded-2xl border border-gray-200/50 dark:border-zinc-800/50">
                {[
                  { mode: "video" as const, icon: <FaFilm />, color: "bg-primary text-white shadow-md" },
                  { mode: "image" as const, icon: <FaImage />, color: "bg-emerald-600 text-white shadow-md" },
                ].map(({ mode, icon, color }) => (
                  <button key={mode} type="button" onClick={() => { setDownloadMode(mode); setMetadata(null); setError(""); }} disabled={isProcessing}
                    className={`p-3.5 rounded-xl text-sm transition-all ${
                      downloadMode === mode ? color : "text-gray-600 dark:text-gray-400 hover:bg-white/60 dark:hover:bg-zinc-800/50"
                    }`}
                    title={mode === "video" ? "Download Video" : "Download Image"}
                  >
                    {icon}
                  </button>
                ))}
              </div>
            </div>
          </form>

          {/* Progress */}
          {isProcessing && (
            <div className="mt-6 pt-6 border-t border-gray-100 dark:border-zinc-800/60 space-y-4 animate-fade-in-up">
              <div className="flex justify-between items-center text-sm font-medium text-gray-600 dark:text-gray-300">
                <span className="flex items-center gap-2"><FaSpinner className="animate-spin text-primary" /> {status}</span>
                <span className="text-primary font-bold text-base">{progress}%</span>
              </div>
              <div className="w-full bg-gray-200/60 dark:bg-zinc-800 rounded-full h-3 overflow-hidden relative">
                <div className="bg-gradient-to-r from-primary to-primary/70 h-3 rounded-full transition-all duration-500 ease-out" style={{ width: `${progress}%` }} />
              </div>
            </div>
          )}

          {error && (
            <div className="mt-4 bg-red-500/10 text-red-600 dark:text-red-400 p-4 rounded-xl border border-red-500/20 font-medium text-sm flex items-center gap-2 animate-fade-in-up">
              <span>&#9888;</span> {error}
            </div>
          )}

          {metadata && !isProcessing && getFilteredOptions(metadata, downloadMode).length === 0 && (
            <div className="mt-4 bg-amber-500/10 text-amber-700 dark:text-amber-400 p-4 rounded-xl border border-amber-500/20 font-medium text-sm">
              Tidak ada {downloadMode === "image" ? "foto" : "video"} yang ditemukan. Coba mode lainnya atau periksa link Anda.
            </div>
          )}
        </div>

        {/* Results */}
        {metadata && !isProcessing && getFilteredOptions(metadata, downloadMode).length > 0 && (
          <ResultsPanel metadata={metadata} onDownload={triggerDownload} />
        )}

        {/* Download bar (fixed bottom, no overlay) */}
        {isDownloading && (
          <div className="fixed bottom-0 left-0 right-0 z-50 bg-white dark:bg-zinc-900 border-t border-gray-200 dark:border-zinc-800 shadow-2xl px-4 py-3">
            <div className="max-w-3xl mx-auto flex items-center gap-4">
              <div className="flex-1 min-w-0 space-y-1.5">
                <div className="flex items-center justify-between gap-4">
                  <p className="text-xs font-medium text-gray-700 dark:text-gray-300 truncate">{downloadStatus}</p>
                  <span className="text-xs font-bold text-primary shrink-0">{downloadProgress}%</span>
                </div>
                <div className="w-full bg-gray-200 dark:bg-zinc-700 rounded-full h-1.5 overflow-hidden">
                  <div className="bg-primary h-1.5 rounded-full transition-all duration-200 ease-linear" style={{ width: `${downloadProgress}%` }} />
                </div>
                {(downloadSpeed || downloadEta) && (
                  <p className="text-[10px] text-gray-400 dark:text-zinc-500">
                    {downloadSpeed && <span>{downloadSpeed}</span>}
                    {downloadSpeed && downloadEta && <span> &middot; </span>}
                    {downloadEta && <span>sisa {downloadEta}</span>}
                  </p>
                )}
              </div>
            </div>
          </div>
        )}

        {/* History */}
        {history.length > 0 && (
          <div className="w-full max-w-3xl space-y-4">
            <div className="flex justify-between items-center">
              <h3 className="text-xl font-serif font-bold text-gray-900 dark:text-white flex items-center gap-2">
                <FaHistory className="text-primary" /> Riwayat
              </h3>
              <button onClick={clearHistory} className="text-red-500 hover:text-red-600 text-sm font-semibold flex items-center gap-1">
                <FaTrash className="text-xs" /> Hapus Semua
              </button>
            </div>
            <div className="grid grid-cols-1 gap-2.5">
              {history.map((item) => (
                <div key={item.id} className="bg-white/50 dark:bg-zinc-900/40 backdrop-blur-md rounded-2xl p-3.5 border border-white/20 dark:border-zinc-800/40 flex items-center justify-between gap-3 shadow-sm hover:shadow-md transition-all">
                  <div className="flex items-center gap-3 min-w-0">
                    <img src={proxyUrl(item.thumbnail, true)} alt=""
                      className="w-14 h-10 object-cover rounded-lg bg-gray-150 border border-gray-200 dark:border-zinc-800 shrink-0"
                      onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = 'none'; }}
                    />
                    <div className="min-w-0">
                      <div className="flex items-center gap-2 mb-0.5">
                        <span className="text-[10px] uppercase font-bold tracking-wider text-primary px-1.5 py-0.5 rounded bg-primary/10">{item.platform}</span>
                        <span className="text-[10px] text-gray-400 font-mono">{item.timestamp}</span>
                      </div>
                      <p className="text-sm font-semibold text-gray-800 dark:text-gray-200 truncate pr-4">{item.title}</p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2 shrink-0">
                    <button onClick={() => { setUrl(item.url); window.scrollTo({ top: 0, behavior: 'smooth' }); }}
                      className="text-xs font-semibold text-primary hover:text-primaryHover border border-primary/20 hover:border-primary/50 px-3.5 py-2 rounded-xl transition-all"
                    >Ulang</button>
                    <button onClick={() => removeHistoryItem(item.id)} className="p-2 text-gray-400 hover:text-red-500 rounded-xl hover:bg-gray-100 dark:hover:bg-zinc-800/60 transition-all" aria-label="Hapus">
                      <FaTrash className="text-xs" />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Guide */}
        <div className="w-full max-w-3xl space-y-6 pt-4">
          <div className="text-center">
            <h3 className="text-2xl font-serif font-bold text-gray-900 dark:text-white">Cara Mengunduh</h3>
            <p className="text-sm text-gray-500 dark:text-gray-400">Ikuti 3 langkah cepat di bawah ini.</p>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {[
              { step: "1", title: "Salin Link", desc: "Buka platform, pilih video/foto dan salin tautannya.", icon: "&#128279;" },
              { step: "2", title: "Tempel & Cari", desc: "Tempel tautan di input dan klik tombol Cari Media.", icon: "&#128229;" },
              { step: "3", title: "Pilih & Simpan", desc: "Pilih format resolusi/kualitas lalu simpan ke perangkat.", icon: "&#128190;" }
            ].map((step, idx) => (
              <div key={idx} className="bg-white/40 dark:bg-zinc-900/20 backdrop-blur-md border border-white/10 dark:border-zinc-800/30 rounded-2xl p-6 text-center hover:scale-[1.02] transition duration-300 shadow-sm">
                <div className="w-12 h-12 bg-primary/10 rounded-full flex items-center justify-center text-primary font-bold text-lg mx-auto mb-4 border border-primary/20">{step.step}</div>
                <div className="text-2xl mb-2" dangerouslySetInnerHTML={{ __html: step.icon }} />
                <h4 className="font-semibold text-gray-900 dark:text-white mb-1">{step.title}</h4>
                <p className="text-xs text-gray-500 dark:text-gray-400 leading-relaxed">{step.desc}</p>
              </div>
            ))}
          </div>
        </div>

        {/* FAQ */}
        <div className="w-full max-w-3xl space-y-5 pt-4">
          <div className="text-center">
            <h3 className="text-2xl font-serif font-bold text-gray-900 dark:text-white flex items-center justify-center gap-2">
              <FaQuestionCircle className="text-primary text-xl" /> FAQ
            </h3>
          </div>
          <div className="space-y-2.5">
            {[
              { q: "Bagaimana cara menyalin link?", a: "Buka platform, klik 'Bagikan' (Share), lalu pilih 'Salin Tautan'." },
              { q: "Apakah layanan ini berbayar?", a: "Gratis 100%, tanpa batasan kuota harian." },
              { q: "Mengapa download gagal?", a: "Pastikan URL valid dan dapat diakses publik. Pastikan server backend berjalan." },
              { q: "Format apa yang tersedia?", a: "Video MP4 hingga 1080p, Audio MP3, dan Cover Photo resolusi penuh." }
            ].map((faq, idx) => (
              <div key={idx} className="bg-white/40 dark:bg-zinc-900/20 backdrop-blur-md rounded-2xl border border-white/20 dark:border-zinc-800/40 overflow-hidden shadow-sm">
                <button onClick={() => setExpandedFaq(expandedFaq === idx ? null : idx)}
                  className="w-full p-4 text-left flex justify-between items-center font-semibold text-gray-800 dark:text-gray-200 hover:bg-primary/5 transition-all text-sm gap-4"
                >
                  <span>{faq.q}</span>
                  {expandedFaq === idx ? <FaChevronUp className="text-primary text-sm shrink-0" /> : <FaChevronDown className="text-primary text-sm shrink-0" />}
                </button>
                {expandedFaq === idx && (
                  <div className="px-4 pb-4 pt-1 text-xs md:text-sm text-gray-500 dark:text-gray-400 border-t border-gray-100/50 dark:border-zinc-800/30 leading-relaxed animate-fade-in-up">{faq.a}</div>
                )}
              </div>
            ))}
          </div>
        </div>

        {/* Footer */}
        <div className="w-full text-center text-xs text-gray-400 dark:text-gray-600 pt-6 pb-4 border-t border-gray-200/30 dark:border-zinc-800/30">
          <p>&copy; {new Date().getFullYear()} Universal Media</p>
        </div>
      </div>
    </div>
  );
}

function getFilteredOptions(metadata: MediaMetadata, mode: string): MediaOption[] {
  if (!metadata || !metadata.options) return [];
  if (mode === "image") {
    return metadata.options.filter((opt) =>
      ["jpg", "jpeg", "png", "webp", "gif"].includes(opt.format.toLowerCase())
    );
  }
  return metadata.options.filter((opt) =>
    !["jpg", "jpeg", "png", "webp", "gif"].includes(opt.format.toLowerCase())
  );
}
