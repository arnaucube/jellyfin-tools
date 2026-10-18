import { test, expect } from '@playwright/test';
const offline = process.env.REEL_E2E_AUTH_MODE === 'offline';

test('private review: login, navigation, years, responsive layout, logout', async ({ page }) => {
  const errors = [];
  const externalRequests = [];
  page.on('request', request => { if (new URL(request.url()).origin !== 'http://127.0.0.1:18180') externalRequests.push(request.url()); });
  page.on('pageerror', e => errors.push(e.message));
  await page.goto('/review/2025');
  await expect(page.getByRole('heading', { name: 'Jellyfin recap', level: 1 })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Your year, replayed.', level: 2 })).toBeVisible();
  await expect(page.locator('header, footer, .login-intro')).toHaveCount(0);
  if (offline) await expect(page.getByText(/checked locally against a private Jellyfin account snapshot/)).toBeVisible();
  await page.getByLabel('Jellyfin username').fill(offline ? 'Alice' : 'fixture');
  await page.getByLabel('Password', { exact: true }).fill('fixture-only');
  await page.getByRole('button', { name: 'Roll the credits' }).click();
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();
  await page.getByRole('button', { name: 'View my recap' }).click();
  await expect(page.getByText('hours watched', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '3. Movies watched', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'The Last Lighthouse' }).first()).toBeVisible();
  const poster = page.getByRole('img', { name: 'Poster for The Last Lighthouse' }).first();
  await expect(poster).toBeVisible();
  await expect(poster).toHaveAttribute('src', /^\/art\/[a-f0-9]{64}\.jpg$/);
  await expect.poll(() => poster.evaluate(image => image.naturalWidth)).toBeGreaterThan(0);
  await page.locator('main').click({ position: { x: 5, y: 5 } });
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('heading', { name: 'Orbit Station' }).first()).toBeVisible();
  await page.getByLabel('Review year').selectOption('2024');
  await expect(page).toHaveURL(/2024$/);
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();
  await page.reload();
  await expect(page.getByLabel('Review year')).toHaveValue('2024');
  for (const label of [/Viewing by hour$/, /Your longest viewing session$/, /Your viewing profile$/, /Your year in summary$/]) {
    await page.getByRole('button', { name: label, exact: true }).click();
    await expect(page.locator('.story')).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  }
  await page.screenshot({ path: 'test-results/review-mobile.png', fullPage: true });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.screenshot({ path: 'test-results/review-desktop.png', fullPage: true });
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByLabel('Jellyfin username')).toBeVisible();
  await expect(page.locator('header, footer, .login-intro')).toHaveCount(0);
  expect((await page.request.get('/api/review?year=2025')).status()).toBe(401);
  expect(errors).toEqual([]);
  expect(externalRequests).toEqual([]);
});


test('unavailable cards are skipped and chapter navigation updates when changing years', async ({ page }) => {
  await page.route('**/api/review?year=*', async route => {
    const response = await route.fetch();
    const review = await response.json();
    for (const availability of Object.values(review.availability)) {
      availability.available = review.year !== 2025;
    }
    await route.fulfill({ response, json: review });
  });
  await page.goto('/review/2025');
  await page.getByLabel('Jellyfin username').fill(offline ? 'Alice' : 'fixture');
  await page.getByLabel('Password', { exact: true }).fill('fixture-only');
  await page.getByRole('button', { name: 'Roll the credits' }).click();
  const chapters = page.getByRole('navigation', { name: 'Review chapters' });
  await expect(chapters.getByRole('button')).toHaveCount(10);
  await expect(chapters.getByRole('button', { name: /Your watch time|Unfinished movies|Your longest viewing session|Your viewing profile|It's not a competition\.\.\. but this is your watch-time ranking/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'View my recap' }).click();
  await expect(page.getByRole('heading', { name: 'Movies watched' })).toBeVisible();
  await page.locator('main').click({ position: { x: 5, y: 5 } });
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('heading', { name: 'TV shows watched' })).toBeVisible();
  await page.getByRole('button', { name: 'Next chapter' }).click();
  await expect(page.getByRole('heading', { name: "Shows you've watched are from platforms..." })).toBeVisible();
  await page.getByRole('button', { name: 'Next chapter' }).click();
  await expect(page.getByRole('heading', { name: 'Your top genres' })).toBeVisible();
  await page.locator('main').click({ position: { x: 5, y: 5 } });
  await page.keyboard.press('End');
  await expect(page.getByRole('heading', { name: 'Your year in summary' })).toBeVisible();
  await expect(page.locator('.story-bottom')).toContainText('10 / 10');
  await expect(page.getByRole('button', { name: 'Next chapter' })).toBeDisabled();
  await expect(page.locator('.stat-grid')).not.toContainText('Your viewing profile');
  await page.getByLabel('Review year').selectOption('2024');
  await expect(chapters.getByRole('button')).toHaveCount(15);
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();
  await chapters.getByRole('button', { name: '5. Unfinished movies', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Unfinished movies' })).toBeVisible();
  await page.getByLabel('Review year').selectOption('2025');
  await expect(chapters.getByRole('button')).toHaveCount(10);
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();
});

test('complete movie and show timelines include every title and support older exports', async ({ page }) => {
  let mode = 'timeline';
  await page.route('**/api/review?year=*', async route => {
    const response = await route.fetch();
    const review = await response.json();
    for (const [field, title] of [['movies', 'Movie'], ['shows', 'Show']]) {
      review[field] = mode === 'empty' ? [] : Array.from({ length: 7 }, (_, i) => ({
        id: `${field}-${i}`, title: `${title} ${i + 1}`, seconds: 3600, records: 1,
        ...(i === 0 && field === 'shows' ? { poster: review.shows[0]?.poster, posterSource: 'https://www.tvmaze.com/shows/42' } : {}),
        ...(mode === 'legacy' ? {} : { months: i === 0 ? [1, 3] : i === 6 ? [12] : [1] })
      }));
    }
    await route.fulfill({ response, json: review });
  });
  await page.goto('/review/2025');
  await page.getByLabel('Jellyfin username').fill(offline ? 'Alice' : 'fixture');
  await page.getByLabel('Password', { exact: true }).fill('fixture-only');
  await page.getByRole('button', { name: 'Roll the credits' }).click();
  for (const [label, title] of [['Movies watched', 'Movie'], ['TV shows watched', 'Show']]) {
    await page.getByRole('button', { name: new RegExp(`${label}$`) }).click();
    await expect(page.locator('.timeline-month > h3')).toHaveText(['January 2025', 'March 2025', 'December 2025']);
    await expect(page.getByRole('region', { name: 'January 2025', exact: true }).getByRole('listitem')).toHaveCount(6);
    await expect(page.getByRole('region', { name: 'March 2025', exact: true }).getByRole('heading', { name: `${title} 1`, exact: true })).toBeVisible();
    await expect(page.getByRole('region', { name: 'December 2025', exact: true }).getByRole('heading', { name: `${title} 7` })).toBeVisible();
    await expect(page.locator('.timeline-titles li')).toHaveCount(8);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  }
  await expect(page.getByRole('link', { name: 'TVmaze', exact: true })).toHaveCount(2);
  await expect(page.getByRole('link', { name: 'TVmaze', exact: true }).first()).toHaveAttribute('href', 'https://www.tvmaze.com/shows/42');
  mode = 'legacy';
  await page.reload();
  await page.getByRole('button', { name: /Movies watched$/ }).click();
  await expect(page.getByRole('region', { name: 'Viewing dates unavailable' }).getByRole('listitem')).toHaveCount(7);
  mode = 'empty';
  await page.reload();
  await page.getByRole('button', { name: /Movies watched$/ }).click();
  await expect(page.getByText('No movies watched in 2025.')).toBeVisible();
  await expect(page.locator('.viewing-timeline')).toHaveCount(0);
});

test('ranking celebrates on entry, clears on exit, and respects reduced motion', async ({ page }) => {
  await page.route('**/api/review?year=*', async route => {
    const response = await route.fetch();
    const review = await response.json();
    review.ranking = { rank: 3, cohort: 6, percentile: 40 };
    review.availability.ranking.available = true;
    await route.fulfill({ response, json: review });
  });
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.goto('/review/2025');
  await page.getByLabel('Jellyfin username').fill(offline ? 'Alice' : 'fixture');
  await page.getByLabel('Password', { exact: true }).fill('fixture-only');
  await page.getByRole('button', { name: 'Roll the credits' }).click();
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();
  const confetti = page.locator('.confetti');
  await expect(confetti).toHaveCount(0);
  await page.getByRole('button', { name: /It's not a competition\.\.\. but this is your watch-time ranking$/ }).click();
  await expect(confetti).toBeVisible();
  await expect(confetti).toHaveAttribute('aria-hidden', 'true');
  expect(await confetti.evaluate(element => {
    const animations = element.getAnimations({ subtree: true });
    if (!animations.length) return false;
    for (const animation of animations) animation.finish();
    return [...element.children].every(piece => piece.getBoundingClientRect().top > innerHeight);
  })).toBeTruthy();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  await page.getByRole('button', { name: 'Next chapter' }).click();
  await expect(confetti).toHaveCount(0);
  await page.getByRole('button', { name: 'Previous chapter' }).click();
  await expect(confetti).toBeVisible();
  expect(await confetti.evaluate(element => element.getAnimations({ subtree: true }).some(animation => animation.playState === 'running'))).toBeTruthy();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(confetti).toBeHidden();
  await expect(page.getByRole('heading', { name: "It's not a competition... but this is your watch-time ranking" })).toBeVisible();
});

test('anonymous ranking highlights your position and handles ties and edge positions', async ({ page }) => {
  let ranking = { rank: 3, cohort: 6, percentile: 40 };
  await page.route('**/api/review?year=*', async route => {
    const response = await route.fetch();
    const review = await response.json();
    review.ranking = ranking;
    review.availability.ranking.available = true;
    await route.fulfill({ response, json: review });
  });
  await page.goto('/review/2025');
  await page.getByLabel('Jellyfin username').fill(offline ? 'Alice' : 'fixture');
  await page.getByLabel('Password', { exact: true }).fill('fixture-only');
  await page.getByRole('button', { name: 'Roll the credits' }).click();
  const chart = page.locator('.ranking-position');
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();
  for (const sample of [
    { rank: 3, cohort: 6, percentile: 40, above: 2, tied: 1, below: 2 },
    { rank: 1, cohort: 6, percentile: 100, above: 0, tied: 0, below: 5 },
    { rank: 6, cohort: 6, percentile: 0, above: 5, tied: 0, below: 0 },
    { rank: 1, cohort: 6, percentile: 0, above: 0, tied: 5, below: 0 },
    { rank: 1, cohort: 1, percentile: 0, above: 0, tied: 0, below: 0 }
  ]) {
    ranking = { rank: sample.rank, cohort: sample.cohort, percentile: sample.percentile };
    await page.reload();
    await page.getByRole('button', { name: /It's not a competition\.\.\. but this is your watch-time ranking$/ }).click();
    await expect(chart.getByRole('listitem')).toHaveCount(sample.cohort);
    await expect(chart.getByRole('listitem', { name: `You: rank ${sample.rank} of ${sample.cohort}`, exact: true })).toHaveCount(1);
    await expect(chart.locator('.above')).toHaveCount(sample.above);
    await expect(chart.locator('.tied')).toHaveCount(sample.tied);
    await expect(chart.locator('.below')).toHaveCount(sample.below);
    await expect(chart).not.toContainText(offline ? 'Alice' : 'Fixture Viewer');
    await expect(chart.locator('a')).toHaveCount(0);
    if (sample.rank === 3) {
      await chart.screenshot({ path: 'test-results/ranking-mobile.png' });
      await expect(page.getByText('Your total watch time compared with other eligible viewers.', { exact: true })).toHaveCount(0);
      const title = page.getByRole('heading', { name: "It's not a competition... but this is your watch-time ranking" });
      await expect(title.locator('span')).toHaveText(["It's not a competition...", 'but this is your watch-time ranking']);
      const headingSizes = await title.locator('span').evaluateAll(elements => elements.map(element => parseFloat(getComputedStyle(element).fontSize)));
      expect(headingSizes[1]).toBeLessThan(headingSizes[0]);
      expect(headingSizes[1]).toBeGreaterThan(headingSizes[0] * .8);
      const highlights = page.locator('.ranking-highlights');
      await expect(highlights.locator('.ranking-value')).toHaveText(['#3', '12 h']);
      for (const width of [390, 1440, 320]) {
        await page.setViewportSize({ width, height: 1000 });
        const metrics = await highlights.locator('.big-stat').evaluateAll(elements => elements.map(element => {
          const box = element.getBoundingClientRect();
          const value = element.querySelector('.ranking-value');
          return { x: box.x, y: box.y, fontSize: parseFloat(getComputedStyle(value).fontSize) };
        }));
        expect(metrics[1].x).toBeGreaterThan(metrics[0].x);
        expect(metrics[1].y).toBe(metrics[0].y);
        const secondarySize = await page.locator('.ranking-secondary b').evaluate(element => parseFloat(getComputedStyle(element).fontSize));
        expect(metrics[1].fontSize).toBeGreaterThan(secondarySize);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
        await page.locator('.story').screenshot({ path: `test-results/ranking-highlights-${width}.png` });
      }
      await page.setViewportSize({ width: 390, height: 844 });
    }
    if (sample.cohort === 1) await expect(chart).toContainText('You are the only eligible viewer.');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  }
});

test('platform card shows only TV show counts and handles missing data', async ({ page }) => {
  let mode = 'populated';
  await page.route('**/api/review?year=*', async route => {
    const response = await route.fetch();
    const review = await response.json();
    if (mode === 'populated' || mode === 'old-label') review.platforms = [
      { name: 'Netflix', movies: 3, shows: 2 },
      { name: 'HBO', movies: 0, shows: 1 },
      { name: 'Apple TV+', movies: 1, shows: 0 },
      { name: 'Other', movies: 20, shows: 1 }
    ];
    else delete review.platforms;
    if (mode === 'old-label') {
      review.platforms.pop();
      review.platforms.unshift({ name: 'Unknown platform', movies: 20, shows: 1 });
    }
    if (mode === 'empty') review.records = 0;
    await route.fulfill({ response, json: review });
  });
  await page.goto('/review/2025');
  await page.getByLabel('Jellyfin username').fill(offline ? 'Alice' : 'fixture');
  await page.getByLabel('Password', { exact: true }).fill('fixture-only');
  await page.getByRole('button', { name: 'Roll the credits' }).click();
  await page.getByRole('button', { name: /Shows you've watched are from platforms\.\.\.$/ }).click();
  const table = page.getByRole('table', { name: 'TV shows watched by platform' });
  await expect(table.getByRole('columnheader')).toHaveText(['Platform', 'TV shows']);
  for (const [name, shows] of [['Netflix', '2'], ['HBO', '1'], ['Apple TV+', '0'], ['Other', '1']]) {
    const row = table.getByRole('row').filter({ has: page.getByRole('rowheader', { name, exact: true }) });
    await expect(row.getByRole('cell')).toHaveText([shows]);
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  await expect(table.getByRole('rowheader').last()).toHaveText('Other');
  mode = 'old-label';
  await page.reload();
  await page.getByRole('button', { name: /Shows you've watched are from platforms\.\.\.$/ }).click();
  await expect(table.getByRole('rowheader')).toHaveText(['Netflix', 'HBO', 'Apple TV+', 'Other']);
  mode = 'legacy';
  await page.reload();
  await page.getByRole('button', { name: /Shows you've watched are from platforms\.\.\.$/ }).click();
  await expect(page.getByText('Platform data is unavailable for this review. Regenerate the year to include it.')).toBeVisible();
  mode = 'empty';
  await page.reload();
  await page.getByRole('button', { name: /Shows you've watched are from platforms\.\.\.$/ }).click();
  await expect(page.getByText('No TV shows watched this year.')).toBeVisible();
});

test('card swipes follow navigation direction and respect reduced motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.goto('/review/2025');
  await page.getByLabel('Jellyfin username').fill(offline ? 'Alice' : 'fixture');
  await page.getByLabel('Password', { exact: true }).fill('fixture-only');
  await page.getByRole('button', { name: 'Roll the credits' }).click();
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();

  // Sample the start of each animation so direction checks do not depend on timing.
  async function entryOffset() {
    return page.locator('.story-body').evaluate(element => {
      const animation = element.getAnimations()[0];
      if (!animation) throw new Error('Expected a card swipe animation');
      animation.pause();
      animation.currentTime = 0;
      const offset = new DOMMatrixReadOnly(getComputedStyle(element).transform).m41;
      animation.finish();
      return offset;
    });
  }
  async function swipe(dx, dy = 0) {
    await page.locator('.story').evaluate((element, { dx, dy }) => {
      element.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch', clientX: 180, clientY: 250 }));
      window.dispatchEvent(new PointerEvent('pointerup', { pointerType: 'touch', clientX: 180 + dx, clientY: 250 + dy }));
    }, { dx, dy });
  }

  await page.getByRole('button', { name: 'Next chapter' }).click();
  await expect(page.getByRole('heading', { name: 'Your watch time', exact: true })).toBeVisible();
  expect(await entryOffset()).toBeGreaterThan(0);
  await page.getByRole('button', { name: 'Previous chapter' }).click();
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();
  expect(await entryOffset()).toBeLessThan(0);

  await swipe(-120);
  await expect(page.getByRole('heading', { name: 'Your watch time', exact: true })).toBeVisible();
  expect(await entryOffset()).toBeGreaterThan(0);
  await swipe(120);
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();
  expect(await entryOffset()).toBeLessThan(0);
  await swipe(-20, 120);
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();

  await page.locator('main').click({ position: { x: 5, y: 5 } });
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('heading', { name: 'Your watch time', exact: true })).toBeVisible();
  expect(await entryOffset()).toBeGreaterThan(0);
  await page.getByRole('button', { name: /Your year in summary$/ }).click();
  await expect(page.getByRole('heading', { name: 'Your year in summary' })).toBeVisible();
  expect(await entryOffset()).toBeGreaterThan(0);
  await page.getByRole('button', { name: 'View recap again ↺' }).click();
  await expect(page.getByRole('heading', { name: 'Your viewing recap' })).toBeVisible();
  expect(await entryOffset()).toBeLessThan(0);
  await swipe(120);
  await expect(page.getByRole('button', { name: 'Previous chapter' })).toBeDisabled();
  expect(await page.locator('.story-body').evaluate(element => element.getAnimations().some(animation => animation.playState === 'running'))).toBe(false);

  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.getByRole('button', { name: 'Next chapter' }).click();
  await expect(page.getByRole('heading', { name: 'Your watch time', exact: true })).toBeVisible();
  expect(await page.locator('.story-body').evaluate(element => element.getAnimations().length)).toBe(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
});
