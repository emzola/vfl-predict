import type { NextConfig } from "next";

const apiBaseUrl = process.env.API_BASE_URL || "http://127.0.0.1:8090";
const isWindowsBuild = process.platform === "win32";

const nextConfig: NextConfig = {
  // Standalone tracing is required by the Linux web image. Next's Windows
  // tracer can fail while copying nested optional packages, so local Windows
  // verification uses the regular production output instead.
  ...(isWindowsBuild ? {} : { output: "standalone" as const }),
  // Next's parallel server compiler can race while creating route output
  // directories on Windows. Keep local/CI builds deterministic there; this
  // only affects build concurrency, not the runtime container.
  ...(isWindowsBuild
    ? {
        experimental: {
          cpus: 1,
          webpackBuildWorker: false,
          staticGenerationMaxConcurrency: 1,
          staticGenerationMinPagesPerWorker: 1,
        },
      }
    : {}),
  async rewrites() {
    return [
      {
        source: "/api/v1/:path*",
        destination: apiBaseUrl + "/api/v1/:path*",
      },
    ];
  },
};

export default nextConfig;
