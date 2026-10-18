import { sveltekit } from '@sveltejs/kit/vite';
import adapter from '@sveltejs/adapter-static';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit({ adapter: adapter({ fallback: 'index.html' }) })],
  server: { proxy: { '/api': 'http://127.0.0.1:8080', '/art': 'http://127.0.0.1:8080' } }
});
