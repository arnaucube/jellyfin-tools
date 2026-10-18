import { test } from 'node:test';
import assert from 'node:assert/strict';
import translations from '../src/lib/translations.json' with { type: 'json' };
import { t, message } from '../src/lib/i18n.js';

// Exercise edits to the same catalog the app imports, restoring it after each test.
function withTranslations(values, run) {
  const catalog = translations.languages[translations.language];
  const original = { ...catalog.strings };
  Object.assign(catalog.strings, values);
  try { run(); }
  finally { catalog.strings = original; }
}

test('top-level language selects English or Catalan labels and formatting', async () => {
  const original = translations.language;
  try {
    for (const [language, label, formatted] of [['en', 'Sign out', '1.5'], ['ca', 'Tanca la sessió', '1,5']]) {
      translations.language = language;
      const active = await import(`../src/lib/i18n.js?selection=${language}`);
      assert.equal(active.t('Sign out'), label);
      assert.equal(new Intl.NumberFormat(active.locale).format(1.5), formatted);
      assert.equal(active.direction, translations.languages[language].direction);
    }
  } finally { translations.language = original; }
});

test('an unknown language reports a configuration error', async () => {
  const original = translations.language;
  translations.language = 'missing';
  try {
    await assert.rejects(import('../src/lib/i18n.js?selection=missing'), /Unknown translation language: missing/);
  } finally { translations.language = original; }
});

test('catalog edits replace labels and allow reordered, repeated placeholders', () => {
  withTranslations({
    'Sign out': 'Sair',
    'You: rank {rank} of {count}': 'Entre {count}, você está em {rank} de {count}'
  }, () => {
    assert.equal(t('Sign out'), 'Sair');
    assert.equal(t('You: rank {rank} of {count}', { rank: 3, count: 6 }), 'Entre 6, você está em 3 de 6');
  });
});

test('missing translations fall back to English and inserted data stays literal', () => {
  assert.equal(t('Unlisted text for {name}', { name: '<b>{year}</b>', year: 2025 }), 'Unlisted text for <b>{year}</b>');
  assert.equal(t('constructor'), 'constructor');
  assert.equal(t('Missing {value}'), 'Missing {value}');
  const catalog = translations.languages[translations.language];
  const original = catalog.strings['Sign out'];
  delete catalog.strings['Sign out'];
  try { assert.equal(t('Sign out'), 'Sign out'); }
  finally { catalog.strings['Sign out'] = original; }
});

test('review messages, profiles and counted warnings use translated strings', () => {
  withTranslations({
    'Night Owl': 'Coruja noturna',
    'Unable to sign in with those credentials.': 'Credenciais inválidas.',
    '{count} records excluded because media metadata was deleted or unavailable.': 'Registros excluídos: {count}.'
  }, () => {
    assert.equal(message('Night Owl'), 'Coruja noturna');
    assert.equal(message('Unable to sign in with those credentials.'), 'Credenciais inválidas.');
    assert.equal(message('12 records excluded because media metadata was deleted or unavailable.'), 'Registros excluídos: 12.');
    assert.equal(message('Custom source description'), 'Custom source description');
    assert.equal(message(undefined), '');
  });
});

test('translations preserve all named placeholders', () => {
  const placeholders = text => [...text.matchAll(/\{\w+\}/g)].map(match => match[0]).sort();
  for (const [language, catalog] of Object.entries(translations.languages)) {
    for (const [english, translated] of Object.entries(catalog.strings)) {
      assert.deepEqual([...new Set(placeholders(translated))], [...new Set(placeholders(english))], `${language}: ${english}`);
    }
  }
});
