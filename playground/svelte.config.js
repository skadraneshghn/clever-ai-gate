import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
const config = {
  kit: {
    adapter: adapter({
      pages: '../internal/playground/dist',
      assets: '../internal/playground/dist',
      fallback: 'index.html',
      precompress: false,
      strict: true
    }),
    paths: {
      base: '/playground'
    }
  }
};

export default config;
