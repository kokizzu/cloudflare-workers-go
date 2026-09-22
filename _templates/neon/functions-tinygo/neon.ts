import { defineConfig } from "@neon/config/v1";

export default defineConfig({
  functions: {
    worker: {
      name: "functions-tinygo",
      source: "./build",
      bundler: "none",
    },
  },
});
