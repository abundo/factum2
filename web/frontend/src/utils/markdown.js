import DOMPurify from 'dompurify'
import { Marked } from 'marked'

function escapeAttr(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
}

// Sibling `foo.md` / `./foo.md#anchor` links in docs/user become in-app
// routes. External URLs stay as-is (new tab). Anything that is not a
// single user-doc slug is left unchanged so install/design links do not
// pretend to be GUI pages.
export function rewriteDocHref(href) {
  if (!href) return href
  if (/^(https?:|mailto:|tel:)/i.test(href)) return href
  if (href.startsWith('#') || href.startsWith('/doc/')) return href

  const hashIndex = href.indexOf('#')
  const path = hashIndex >= 0 ? href.slice(0, hashIndex) : href
  const hash = hashIndex >= 0 ? href.slice(hashIndex) : ''
  const slug = path.replace(/^\.\//, '').replace(/\.md$/i, '')
  if (!/^[a-z][a-z0-9-]*$/.test(slug)) return href
  return `/doc/${slug}${hash}`
}

const marked = new Marked()
marked.use({
  gfm: true,
  renderer: {
    link({ href, title, tokens }) {
      const text = this.parser.parseInline(tokens)
      const next = rewriteDocHref(href)
      const titleAttr = title ? ` title="${escapeAttr(title)}"` : ''
      const extra = /^(https?:)/i.test(next) ? ' target="_blank" rel="noopener noreferrer"' : ''
      return `<a href="${escapeAttr(next)}"${titleAttr}${extra}>${text}</a>`
    },
  },
})

export function renderDocMarkdown(markdown) {
  const html = marked.parse(markdown ?? '', { async: false })
  return DOMPurify.sanitize(html, { USE_PROFILES: { html: true } })
}
