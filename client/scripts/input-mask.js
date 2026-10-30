import { PUBLIC_KEY_LENGTH } from './parse-link.js';

const prefixes = ['disk.yandex.ru/d/', 'disk.yandex.ru/i/', 'disk.yandex.com/d/', 'disk.yandex.com/i/'];
const id = new RegExp(`^[A-Za-z0-9_-]{0,${PUBLIC_KEY_LENGTH}}$`);

export function matchesInputMask(value) {
  return prefixes.some(prefix => prefix.startsWith(value)
    || value.startsWith(prefix) && id.test(value.slice(prefix.length)));
}

export function normalizePastedLink(value) {
  const text = value.trim().replace(/^https?:\/\//i, '')
    .replace(/^disk\.yandex\.(ru|com)(?=\/)/i, host => host.toLowerCase());
  return /^disk\.yandex\.(ru|com)\/[di]\//.test(text) ? text.split(/[?#]/, 1)[0] : text;
}

export function bindInputMask(input) {
  let acceptedValue = input.value;
  let acceptedCaret = input.selectionStart ?? 0;

  const candidate = text => input.value.slice(0, input.selectionStart ?? 0)
    + text + input.value.slice(input.selectionEnd ?? input.value.length);

  input.addEventListener('beforeinput', (event) => {
    if (input.readOnly || !event.inputType.startsWith('insert')) return;
    const text = event.data ?? event.dataTransfer?.getData('text/plain');
    if (text === null || text === undefined) return;
    if (!matchesInputMask(candidate(text))) event.preventDefault();
  });

  input.addEventListener('paste', (event) => {
    if (input.readOnly) return;
    const text = event.clipboardData?.getData('text/plain');
    if (!text) return;
    event.preventDefault();
    const normalized = normalizePastedLink(text);
    if (!matchesInputMask(candidate(normalized))) return;
    input.setRangeText(normalized, input.selectionStart ?? 0, input.selectionEnd ?? input.value.length, 'end');
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });

  // Some autofill/IME operations are not cancellable. Keep their values inside the same mask.
  input.addEventListener('input', () => {
    if (!matchesInputMask(input.value)) {
      const normalized = normalizePastedLink(input.value);
      if (matchesInputMask(normalized)) input.value = normalized;
      else {
        input.value = acceptedValue;
        input.setSelectionRange(acceptedCaret, acceptedCaret);
        return;
      }
    }
    acceptedValue = input.value;
    acceptedCaret = input.selectionStart ?? input.value.length;
  });
}
