import { defineConfig, godoc } from "sourcey";

export default defineConfig({
  name: "go-smtp",
  repo: "https://github.com/emersion/go-smtp",
  theme: {
    preset: "api-first",
    colors: {
      primary: "#2563eb",
    },
  },
  navigation: {
    tabs: [
      {
        tab: "Go API",
        slug: "",
        source: godoc({ module: ".", packages: ["./..."] }),
      },
    ],
  },
  navbar: {
    links: [
      { type: "github", href: "https://github.com/emersion/go-smtp" },
    ],
  },
  footer: {
    links: [
      { type: "github", href: "https://github.com/emersion/go-smtp" },
    ],
  },
});
