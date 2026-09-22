import { defineConfig } from "blume";

export default defineConfig({
  title: "workers-go",
  description:
    "Run HTTP servers written in Go on Cloudflare Workers and in the browser.",
  github: {
    owner: "syumai",
    repo: "workers-go",
    dir: "docs",
  },
  content: {
    root: "content",
  },
});
