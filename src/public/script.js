const correctUrl = (url) => url.replace(/(?<!:)\/+/gm, '/')

const rootDomain = (hostname) => {
  let parts = hostname.split('.')
  if (parts.length <= 2) return hostname
  parts = parts.slice(-3);
  return parts.slice(-2).join('.')
}

if (window.location.pathname != '/') window.location.href = '/'

const domain = rootDomain(window.location.hostname) + (window.location.port == '' ? '' : `:${window.location.port}`)

document.querySelector('.note').innerText = `or change 'yandex.ru' to '${domain}' in URL`

document.querySelector('#text').addEventListener('keypress', (event) => {
  if (event.key === 'Enter') {
    const text = document.querySelector('#text')
    const url = correctUrl(`/${text.value.trim()}`)
    text.value = ''
    window.location.href = url
  }
})
