/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Subtle minimal palette; refine when adding shadcn.
        bg: "#0b0c0f",
        panel: "#13151a",
        border: "#222731",
        muted: "#8a93a6",
        text: "#e6e9ef",
        accent: "#a6c1ff",
        warn: "#f0b466",
        success: "#7ee787",
        danger: "#ff7b72",
      },
      fontFamily: {
        sans: ["Inter", "system-ui", "sans-serif"],
        mono: ["JetBrains Mono", "ui-monospace", "monospace"],
      },
    },
  },
  plugins: [],
};
