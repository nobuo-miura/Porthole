/** @type {import('tailwindcss').Config} */
// リポジトリルートから `make tailwind` で実行する前提のパス。
module.exports = {
  content: ['./web/**/*.html', './web/**/*.js'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        brand: '#3B82F6',
      },
    },
  },
};
