import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)
const organizationName = 'saskia-peters';
const projectName = 'gear';

const config: Config = {
  title: 'G.E.A.R.',
  tagline: 'Operational equipment management for the Ortsverband Singen',
  favicon: 'img/logo.svg',

  future: {
    v4: true,
  },

  // Set the production url of your site here
  url: `https://${organizationName}.github.io`,
  baseUrl: `/${projectName}/`,

  // GitHub pages deployment config.
  organizationName,
  projectName,

  // GitHub Pages serves the subpath without a trailing-slash check unless set
  trailingSlash: true,

  onBrokenLinks: 'throw',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          routeBasePath: 'docs',
          editUrl: `https://github.com/${organizationName}/${projectName}/tree/main/docs/`,
          // Required by docusaurus-theme-openapi-docs: API pages render through
          // @theme/ApiItem, not the default DocItem (which reads a docs-theme
          // store that is null for API items -> "Cannot destructure property
          // 'store' of ... as it is null").
          docItemComponent: '@theme/ApiItem',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  plugins: [
    [
      'docusaurus-plugin-openapi-docs',
      {
        id: 'api',
        docsPluginId: 'classic',
        config: {
          gear: {
            specPath: 'openapi/openapi.yaml',
            outputDir: 'docs/api',
            sidebarOptions: {
              groupPathsBy: 'tag',
            },
          } satisfies any,
        },
      },
    ],
  ],

  themes: ['docusaurus-theme-openapi-docs', '@docusaurus/theme-mermaid'],
  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  themeConfig: {
    image: 'img/docusaurus-social-card.jpg',
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'G.E.A.R.',
      logo: {
        alt: 'G.E.A.R. Logo',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          type: 'docSidebar',
          sidebarId: 'apiSidebar',
          position: 'left',
          label: 'API',
        },
        {
          href: `https://github.com/${organizationName}/${projectName}`,
          label: 'GitHub',
          position: 'right',
        },
        {
          type: 'doc',
          docId: 'management-overview',
          label: 'Overview',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {
              label: 'Intro',
              to: '/docs/intro',
            },
            {
              label: 'PRD',
              to: '/docs/planning/prd',
            },
            {
              label: 'Architecture Spine',
              to: '/docs/planning/architecture-spine',
            },
          ],
        },
        {
          title: 'Source',
          items: [
            {
              label: 'GitHub',
              href: `https://github.com/${organizationName}/${projectName}`,
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} G.E.A.R. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
