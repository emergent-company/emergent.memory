import { chromium } from 'playwright-core';
const CHROME = '/usr/local/bin/chromium';
const URL = 'http://127.0.0.1:4321/emergent.memory/tutorials/deploy-to-railway/';

for (const [label, vp] of [['desktop', { width: 1440, height: 1000 }], ['laptop', { width: 1024, height: 800 }], ['phone', { width: 390, height: 844 }]]) {
  const b = await chromium.launch({ executablePath: CHROME });
  const ctx = await b.newContext({ viewport: vp, colorScheme: 'dark', deviceScaleFactor: 2 });
  const p = await ctx.newPage();
  await p.goto(URL, { waitUntil: 'load' });
  // Force every lazy figure to load so naturalWidth is real.
  await p.evaluate(async () => {
    for (const img of document.querySelectorAll('.screenshot-open img')) {
      img.loading = 'eager';
      img.scrollIntoView({ block: 'center' });
      await img.decode().catch(() => {});
    }
    window.scrollTo(0, 0);
  });
  await p.waitForTimeout(400);

  const cards = await p.evaluate(() =>
    [...document.querySelectorAll('.screenshot-open')].map((btn) => {
      const img = btn.querySelector('img');
      const r = img.getBoundingClientRect();
      return { name: img.getAttribute('src').split('/').pop(), natural: img.naturalWidth, shown: Math.round(r.width) };
    }),
  );
  const upscaled = cards.filter((c) => c.natural && c.shown > c.natural + 0.5).map((c) => `${c.name} ${c.natural}->${c.shown}`);
  const unloaded = cards.filter((c) => !c.natural).map((c) => c.name);

  const idx = cards.findIndex((c) => c.name.startsWith('09'));
  await p.locator('.screenshot-open').nth(idx).click();
  await p.waitForTimeout(400);
  const dlg = await p.evaluate(() => {
    const d = document.querySelector('dialog.screenshot-dialog');
    const dr = d.getBoundingClientRect();
    const img = d.querySelector('img');
    const ir = img.getBoundingClientRect();
    return {
      gaps: [Math.round(dr.left), Math.round(innerWidth - dr.right), Math.round(dr.top), Math.round(innerHeight - dr.bottom)],
      dlg: [Math.round(dr.width), Math.round(dr.height)],
      imgShown: [Math.round(ir.width), Math.round(ir.height)],
      natural: [img.naturalWidth, img.naturalHeight],
      overflowX: document.documentElement.scrollWidth - innerWidth,
    };
  });
  console.log(label, JSON.stringify({ upscaled, unloaded, dlg }));
  await p.screenshot({ path: `/tmp/opencode/dialog-${label}.png` });
  await b.close();
}
