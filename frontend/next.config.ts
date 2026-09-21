import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Standalone output keeps the runtime image small in docker.
  output: "standalone",
};

export default nextConfig;
