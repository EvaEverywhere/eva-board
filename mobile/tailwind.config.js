/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: "class",
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}", "./providers/**/*.{ts,tsx}"],
  presets: [require("nativewind/preset")],
  theme: {
    extend: {
      colors: {
        background: "#06070A",
        foreground: "#F8FAFC",
        card: "#0F1115",
        border: "#1F2630",
        primary: "#00D4AA",
        secondary: "#1A1F27",
        muted: "#9AA4B2",
        destructive: "#FF5A67"
      },
      borderRadius: {
        lg: "14px",
        md: "10px",
        sm: "8px"
      }
    }
  },
  plugins: []
};
