// @ts-check

import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import tseslint from "typescript-eslint";

export default defineConfig({
  files: ["src/**/*.ts"],
  extends: [js.configs.recommended, tseslint.configs.recommended],
  languageOptions: {
    globals: {
      console: "readonly",
      crypto: "readonly",
      fetch: "readonly",
      Headers: "readonly",
      HTMLRewriter: "readonly",
      Request: "readonly",
      Response: "readonly",
      setTimeout: "readonly",
      URL: "readonly",
    },
  },
});
