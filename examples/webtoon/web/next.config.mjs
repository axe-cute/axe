/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  reactStrictMode: true,
  images: {
    remotePatterns: [
      { protocol: "https", hostname: "picsum.photos" },
      { protocol: "https", hostname: "fastly.picsum.photos" },
      { protocol: "https", hostname: "images.unsplash.com" },
      // MinIO (local dev). In prod replace with your CDN host, e.g.
      //   { protocol: "https", hostname: "cdn.example.com" }
      { protocol: "http", hostname: "localhost", port: "9000" },
      { protocol: "http", hostname: "127.0.0.1", port: "9000" },
    ],
  },
  async rewrites() {
    // Prefer the server-side internal URL (read at runtime on the Next.js
    // server, e.g. "http://api:8080" in Docker). Falls back to the public
    // URL or localhost for non-Docker dev. NEXT_PUBLIC_* values are inlined
    // at build time and unreliable inside containers.
    const api =
      process.env.NEXT_INTERNAL_API_URL ||
      process.env.NEXT_PUBLIC_API_URL ||
      "http://localhost:8080";
    // Allow same-origin calls from the browser → avoid CORS for cookies.
    return [{ source: "/api/:path*", destination: `${api}/api/:path*` }];
  },
};

export default nextConfig;
