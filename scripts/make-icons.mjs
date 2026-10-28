// Writes neutral placeholder app icons, favicon and splash image: a plain
// colour and the product initials, from apps/app/brand.json. No logos.
// Usage: npm run icons   (renders SVG to PNG with Playwright's Chromium)
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from '@playwright/test';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const brand = JSON.parse(readFileSync(join(root, 'apps/app/brand.json'), 'utf8'));
const outDir = join(root, 'apps/app/assets/images');
const { background, foreground } = brand.colors;

/** An SVG square with optional background and centred initials. */
function svg(size, { bg, fg, scale }) {
  const font = Math.round(size * scale);
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
    ${bg ? `<rect width="100%" height="100%" fill="${bg}"/>` : ''}
    ${
      fg
        ? `<text x="50%" y="50%" dominant-baseline="central" text-anchor="middle"
      font-family="Helvetica, Arial, sans-serif" font-weight="700" font-size="${font}"
      letter-spacing="${-font * 0.04}" fill="${fg}">${brand.initials}</text>`
        : ''
    }
  </svg>`;
}

// Android adaptive icons keep content inside the central ~66%, hence the
// smaller text on the foreground and monochrome layers.
const ICONS = [
  ['icon.png', 1024, { bg: background, fg: foreground, scale: 0.36 }],
  ['favicon.png', 48, { bg: background, fg: foreground, scale: 0.38 }],
  ['splash-icon.png', 512, { fg: foreground, scale: 0.36 }],
  ['android-icon-background.png', 1024, { bg: background }],
  ['android-icon-foreground.png', 1024, { fg: foreground, scale: 0.24 }],
  ['android-icon-monochrome.png', 1024, { fg: '#FFFFFF', scale: 0.24 }],
];

// Web install assets, served from the site root (apps/app/public). Maskable
// icons keep the initials inside the central 80% safe zone.
const pubDir = join(root, 'apps/app/public');
mkdirSync(join(pubDir, 'icons'), { recursive: true });
const PWA_ICONS = [
  ['icons/icon-192.png', 192, { bg: background, fg: foreground, scale: 0.36 }],
  ['icons/icon-512.png', 512, { bg: background, fg: foreground, scale: 0.36 }],
  ['icons/maskable-512.png', 512, { bg: background, fg: foreground, scale: 0.28 }],
  ['icons/apple-touch-icon.png', 180, { bg: background, fg: foreground, scale: 0.36 }],
];

writeFileSync(
  join(pubDir, 'manifest.webmanifest'),
  JSON.stringify(
    {
      name: brand.name,
      short_name: brand.initials,
      description: 'Fantasy Premier League with your friends.',
      start_url: '/',
      scope: '/',
      id: '/',
      display: 'browser',
      background_color: background,
      theme_color: background,
      icons: [
        { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png', purpose: 'any' },
        { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
        {
          src: '/icons/maskable-512.png',
          sizes: '512x512',
          type: 'image/png',
          purpose: 'maskable',
        },
      ],
    },
    null,
    2,
  ) + '\n',
);
console.log('wrote manifest.webmanifest');

const browser = await chromium.launch();
try {
  for (const [name, size, opts] of PWA_ICONS) {
    const page = await browser.newPage({ viewport: { width: size, height: size } });
    await page.setContent(
      `<html><body style="margin:0;background:transparent">${svg(size, opts)}</body></html>`,
    );
    await page.locator('svg').screenshot({ path: join(pubDir, name), omitBackground: true });
    await page.close();
    console.log(`wrote public/${name} (${size}x${size})`);
  }
  for (const [name, size, opts] of ICONS) {
    const page = await browser.newPage({ viewport: { width: size, height: size } });
    await page.setContent(
      `<html><body style="margin:0;background:transparent">${svg(size, opts)}</body></html>`,
    );
    await page.locator('svg').screenshot({ path: join(outDir, name), omitBackground: true });
    await page.close();
    console.log(`wrote ${name} (${size}x${size})`);
  }
} finally {
  await browser.close();
}
