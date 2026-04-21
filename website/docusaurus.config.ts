import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'EvaBoard',
  tagline:
    'Autonomous dev board — builds, verifies, reviews, and ships code without you in the loop',
  favicon: 'img/favicon.ico',

  future: {
    v4: true,
  },

  url: 'https://evaeverywhere.github.io',
  baseUrl: '/eva-board/',

  organizationName: 'EvaEverywhere',
  projectName: 'eva-board',
  trailingSlash: false,

  onBrokenLinks: 'warn',

  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          // Source markdown from the top-level /docs directory so the
          // shell-readable files double as the website's source.
          path: '../docs',
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          editUrl:
            'https://github.com/EvaEverywhere/eva-board/edit/main/docs/',
          include: ['**/*.md', '**/*.mdx'],
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/docusaurus-social-card.jpg',
    colorMode: {
      defaultMode: 'dark',
      disableSwitch: false,
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'EvaBoard',
      items: [
        {to: '/', label: 'Docs', position: 'left'},
        {
          href: 'https://github.com/EvaEverywhere/eva-board',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Project',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/EvaEverywhere/eva-board',
            },
            {
              label: 'License',
              href: 'https://github.com/EvaEverywhere/eva-board/blob/main/LICENSE',
            },
            {
              label: 'Code of Conduct',
              href: 'https://github.com/EvaEverywhere/eva-board/blob/main/CODE_OF_CONDUCT.md',
            },
          ],
        },
        {
          title: 'Docs',
          items: [
            {label: 'Quickstart', to: '/quickstart'},
            {label: 'Architecture', to: '/architecture'},
            {label: 'Self-hosting', to: '/self-hosting'},
            {label: 'Mobile', to: '/mobile'},
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} EvaEverywhere. Apache-2.0.`,
    },
    prism: {
      theme: prismThemes.oneDark,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
