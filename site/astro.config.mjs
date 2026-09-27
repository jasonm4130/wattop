// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://wattop.app',
  trailingSlash: 'ignore',
  build: { inlineStylesheets: 'never' },
  // Never inline scripts: the CSP is script-src 'self' (see public/_headers).
  vite: { build: { assetsInlineLimit: 0 } },
  integrations: [
    starlight({
      title: 'wattop',
      description:
        'Docs for wattop, a terminal monitor for Apple Silicon Macs that watches Claude Code and Codex sessions and the hardware running them.',
      disable404Route: true,
      favicon: '/favicon.svg',
      customCss: ['./src/styles/fonts.css', './src/styles/tokens.css', './src/styles/starlight.css'],
      components: {
        ThemeProvider: './src/components/starlight/ThemeProvider.astro',
        ThemeSelect: './src/components/starlight/ThemeSelect.astro',
        SiteTitle: './src/components/starlight/SiteTitle.astro',
      },
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/jasonm4130/wattop' }],
      head: [
        { tag: 'meta', attrs: { property: 'og:image', content: 'https://wattop.app/og.png' } },
        { tag: 'meta', attrs: { property: 'og:image:width', content: '1280' } },
        { tag: 'meta', attrs: { property: 'og:image:height', content: '640' } },
        { tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
        {
          tag: 'link',
          attrs: {
            rel: 'preload',
            href: '/fonts/jetbrains-mono-latin-wght.woff2',
            as: 'font',
            type: 'font/woff2',
            crossorigin: '',
          },
        },
      ],
      sidebar: [
        {
          label: 'Start',
          items: [
            { label: 'Overview', link: '/docs/' },
            { label: 'Install', link: '/docs/install/' },
            { label: 'Usage', link: '/docs/usage/' },
            { label: 'Privacy', link: '/docs/privacy/' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Doctor', link: '/docs/doctor/' },
            { label: 'Config file', link: '/docs/config/' },
            { label: 'Themes', link: '/docs/themes/' },
            { label: 'Honest labelling', link: '/docs/honest-labelling/' },
            { label: 'Limitations', link: '/docs/limitations/' },
          ],
        },
      ],
    }),
  ],
});
