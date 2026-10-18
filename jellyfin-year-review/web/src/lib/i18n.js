import translations from './translations.json' with { type: 'json' };

// Select one catalog for the entire build, including page language and formatting.
if (!Object.hasOwn(translations.languages, translations.language)) {
  throw new Error(`Unknown translation language: ${translations.language}`);
}
const catalog = translations.languages[translations.language];
export const locale = catalog.locale;
export const direction = catalog.direction;

// Replace named placeholders once, keeping inserted names and titles as plain text.
export function t(english, values = {}) {
  const translated = Object.hasOwn(catalog.strings, english) ? catalog.strings[english] : english;
  return translated.replace(/\{(\w+)\}/g, (placeholder, name) => Object.hasOwn(values, name) ? String(values[name]) : placeholder);
}

const countedMessages = [
  '{count} records excluded because media metadata was deleted or unavailable.',
  '{count} aggregate activity records; records may combine resumed viewing.'
];

// Review exports contain English messages; translate them without changing stored data.
export function message(english) {
  if (!english) return '';
  for (const template of countedMessages) {
    const suffix = template.slice('{count}'.length);
    if (english.endsWith(suffix)) {
      const count = english.slice(0, -suffix.length);
      if (/^\d+$/.test(count)) return t(template, { count: new Intl.NumberFormat(locale).format(Number(count)) });
    }
  }
  return t(english);
}
