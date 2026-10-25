import type { Config } from "tailwindcss";

const config: Config = {
  darkMode: "class",
  content: ["./app/**/*.{ts,tsx}", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        ink: "#08111f",
        panel: "#101b2d",
        panelMuted: "#14233a",
        accent: "#65e6bb",
        warning: "#f5c76a",
        danger: "#ff7f91",
      },
      boxShadow: {
        panel: "0 20px 60px rgba(0, 0, 0, 0.22)",
      },
    },
  },
  plugins: [],
};

export default config;
