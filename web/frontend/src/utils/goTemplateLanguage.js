import { StreamLanguage } from '@codemirror/language'
import { tags as t } from '@lezer/highlight'

export const TEMPLATE_KEYWORDS = [
  'if',
  'else',
  'end',
  'range',
  'with',
  'define',
  'template',
  'block',
  'break',
  'continue',
]

export const TEMPLATE_BUILTINS = [
  'and',
  'or',
  'not',
  'eq',
  'ne',
  'lt',
  'le',
  'gt',
  'ge',
  'index',
  'slice',
  'len',
  'print',
  'printf',
  'println',
  'html',
  'js',
  'urlquery',
  'call',
]

const KEYWORD_SET = new Set(TEMPLATE_KEYWORDS)
const BUILTIN_SET = new Set(TEMPLATE_BUILTINS)

export const templateHighlightTags = {
  brace: t.special(t.brace),
  keyword: t.keyword,
  func: t.function(t.variableName),
  property: t.propertyName,
  variable: t.variableName,
  string: t.string,
  comment: t.comment,
  number: t.number,
  operator: t.operator,
}

function atActionOpen(stream) {
  return stream.string.startsWith('{{', stream.pos)
}

function atActionClose(stream) {
  const rest = stream.string.slice(stream.pos)
  return rest.startsWith('-}}') || rest.startsWith('}}')
}

export const goTemplateLanguage = StreamLanguage.define({
  name: 'go-template',
  startState() {
    return {
      inAction: false,
      inComment: false,
      actionString: null,
      hostString: false,
    }
  },
  copyState(state) {
    return { ...state }
  },
  token(stream, state) {
    if (!state.inAction) {
      return tokenHost(stream, state)
    }
    return tokenAction(stream, state)
  },
  languageData: {
    commentTokens: { block: { open: '{{/*', close: '*/}}' } },
    closeBrackets: { brackets: ['(', '[', '{', "'", '"', '`'] },
  },
  tokenTable: {
    'action-brace': templateHighlightTags.brace,
    'action-keyword': templateHighlightTags.keyword,
    'action-func': templateHighlightTags.func,
    'action-property': templateHighlightTags.property,
    'action-var': templateHighlightTags.variable,
    'action-string': templateHighlightTags.string,
    'action-comment': templateHighlightTags.comment,
    'action-number': templateHighlightTags.number,
    'action-operator': templateHighlightTags.operator,
    'host-string': templateHighlightTags.string,
    'host-comment': templateHighlightTags.comment,
  },
})

function tokenHost(stream, state) {
  if (stream.match('{{')) {
    stream.match('-')
    state.inAction = true
    state.inComment = false
    state.actionString = null
    return 'action-brace'
  }

  if (state.hostString) {
    if (stream.match('"')) {
      state.hostString = false
      return 'host-string'
    }
    while (!stream.eol() && stream.peek() !== '"' && !atActionOpen(stream)) {
      stream.next()
    }
    return stream.current() ? 'host-string' : null
  }

  if (stream.match('"')) {
    state.hostString = true
    return 'host-string'
  }

  if (stream.match('//')) {
    stream.skipToEnd()
    return 'host-comment'
  }

  while (!stream.eol()) {
    const ch = stream.peek()
    if (ch === '"' || atActionOpen(stream)) {
      break
    }
    if (ch === '/' && stream.string.startsWith('//', stream.pos)) {
      break
    }
    stream.next()
  }
  return null
}

function tokenAction(stream, state) {
  if (state.inComment) {
    if (stream.match('*/')) {
      state.inComment = false
      return 'action-comment'
    }
    stream.next()
    return 'action-comment'
  }

  if (state.actionString) {
    const quote = state.actionString
    if (quote === '"' && stream.match('\\')) {
      if (!stream.eol()) stream.next()
      return 'action-string'
    }
    if (stream.eat(quote)) {
      state.actionString = null
      return 'action-string'
    }
    stream.next()
    return 'action-string'
  }

  if (stream.eatSpace()) {
    return null
  }

  if (stream.match('/*')) {
    state.inComment = true
    return 'action-comment'
  }

  if (stream.match('-}}') || stream.match('}}')) {
    state.inAction = false
    return 'action-brace'
  }

  if (stream.peek() === '-' && atActionClose(stream)) {
    stream.next()
    return 'action-operator'
  }

  if (stream.match('"') || stream.match('`')) {
    state.actionString = stream.current()
    return 'action-string'
  }

  if (stream.match(/^\$\w*/)) {
    return 'action-var'
  }
  if (stream.match(/^\.\w*/)) {
    return 'action-property'
  }
  if (stream.match(/^(true|false|nil)\b/)) {
    return 'action-keyword'
  }
  if (stream.match(/^\d+(\.\d+)?/)) {
    return 'action-number'
  }
  if (stream.match(/^[A-Za-z_]\w*/)) {
    const word = stream.current()
    if (KEYWORD_SET.has(word)) {
      return 'action-keyword'
    }
    if (BUILTIN_SET.has(word)) {
      return 'action-func'
    }
    return 'action-func'
  }
  if (stream.match(/^[|()[\]:=,]/)) {
    return 'action-operator'
  }

  stream.next()
  return null
}

export function isInAction(text, pos = text.length) {
  const end = Math.min(pos, text.length)
  let i = 0
  let inAction = false
  let inComment = false
  let actionString = null
  while (i < end) {
    if (!inAction) {
      if (text.startsWith('{{', i)) {
        inAction = true
        inComment = false
        actionString = null
        i += 2
        continue
      }
      i += 1
      continue
    }
    if (inComment) {
      if (text.startsWith('*/', i)) {
        inComment = false
        i += 2
        continue
      }
      i += 1
      continue
    }
    if (actionString) {
      if (actionString === '"' && text[i] === '\\') {
        i += 2
        continue
      }
      if (text[i] === actionString) {
        actionString = null
      }
      i += 1
      continue
    }
    if (text.startsWith('/*', i)) {
      inComment = true
      i += 2
      continue
    }
    if (text[i] === '"' || text[i] === '`') {
      actionString = text[i]
      i += 1
      continue
    }
    if (text.startsWith('-}}', i)) {
      inAction = false
      i += 3
      continue
    }
    if (text.startsWith('}}', i)) {
      inAction = false
      i += 2
      continue
    }
    i += 1
  }
  return inAction
}

export function findTemplateIssues(text) {
  const value = text ?? ''
  let i = 0
  let inAction = false
  let inComment = false
  let actionString = null
  let actionStart = -1
  while (i < value.length) {
    if (!inAction) {
      if (value.startsWith('{{', i)) {
        inAction = true
        inComment = false
        actionString = null
        actionStart = i
        i += 2
        continue
      }
      i += 1
      continue
    }
    if (inComment) {
      if (value.startsWith('*/', i)) {
        inComment = false
        i += 2
        continue
      }
      i += 1
      continue
    }
    if (actionString) {
      if (actionString === '"' && value[i] === '\\') {
        i += 2
        continue
      }
      if (value[i] === actionString) {
        actionString = null
      }
      i += 1
      continue
    }
    if (value.startsWith('/*', i)) {
      inComment = true
      i += 2
      continue
    }
    if (value[i] === '"' || value[i] === '`') {
      actionString = value[i]
      i += 1
      continue
    }
    if (value.startsWith('-}}', i)) {
      inAction = false
      i += 3
      continue
    }
    if (value.startsWith('}}', i)) {
      inAction = false
      i += 2
      continue
    }
    i += 1
  }
  if (inComment) {
    return 'Unclosed {{/* comment'
  }
  if (actionString) {
    return `Unclosed ${actionString} string in {{ ... }}`
  }
  if (inAction) {
    return `Unclosed {{ action starting at character ${actionStart + 1}`
  }
  return ''
}

export function snippetForItem(item) {
  if (item.insert) {
    return item.insert
  }
  if (item.args) {
    return `{{ ${item.name} ${item.args} }}`
  }
  return `{{ ${item.name} }}`
}

export function unwrapActionSnippet(snippet) {
  return snippet.replace(/^\{\{-?\s*/, '').replace(/\s*-?\}\}$/, '')
}
