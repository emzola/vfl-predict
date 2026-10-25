import type { NextConfig } from "next";

const apiBaseUrl = process.env.API_BASE_URL || "http://127.0.0.1:8090";

const nextConfig: NextConfig = {
  output: "standalone",
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
