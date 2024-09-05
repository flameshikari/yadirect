const text = document.querySelector('#text');

const correctUrl = (url) => url.replace(/^(https?:\/\/)?.*disk.yandex.ru\//gm, '/');

const rootDomain = (hostname) => {
  let parts = hostname.split('.');
  if (parts.length <= 2) return hostname;
  parts = parts.slice(-3);
  return parts.slice(-2).join('.');
};

const domain = rootDomain(window.location.hostname) + (window.location.port == '' ? '' : `:${window.location.port}`);

text.addEventListener('keypress', (event) => {
    if (event.key === 'Enter') {
        const url = correctUrl(`/${text.value.trim()}`);
        text.value = '';
        window.location.href = url;
    };
});
