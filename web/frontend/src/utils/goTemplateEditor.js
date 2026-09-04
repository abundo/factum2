import {
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  completionKeymap,
} from '@codemirror/autocomplete'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { bracketMatching, syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { highlightSelectionMatches, searchKeymap } from '@codemirror/search'
import { Compartment, EditorState, Prec } from '@codemirror/state'
import {
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
  placeholder as placeholderExt,
} from '@codemirror/view'
import {
  TEMPLATE_BUILTINS,
  TEMPLATE_KEYWORDS,
  goTemplateLanguage,
  isInAction,
  templateHighlightTags as tags,
  unwrapActionSnippet,
} from './goTemplateLanguage'

const lightHighlight = HighlightStyle.define([
  { tag: tags.brace, color: '#a21caf', fontWeight: 'bold' },
  { tag: tags.keyword, color: '#7c3aed' },
  { tag: tags.func, color: '#0f766e' },
  { tag: tags.property, color: '#0369a1' },
  { tag: tags.variable, color: '#c2410c' },
  { tag: tags.string, color: '#166534' },
  { tag: tags.comment, color: '#6b7280', fontStyle: 'italic' },
  { tag: tags.number, color: '#b45309' },
  { tag: tags.operator, color: '#9333ea' },
])

const darkHighlight = HighlightStyle.define([
  { tag: tags.brace, color: '#e879f9', fontWeight: 'bold' },
  { tag: tags.keyword, color: '#c4b5fd' },
  { tag: tags.func, color: '#5eead4' },
  { tag: tags.property, color: '#7dd3fc' },
  { tag: tags.variable, color: '#fdba74' },
  { tag: tags.string, color: '#86efac' },
  { tag: tags.comment, color: '#9ca3af', fontStyle: 'italic' },
  { tag: tags.number, color: '#fbbf24' },
  { tag: tags.operator, color: '#d8b4fe' },
])

function chromeTheme(dark) {
  return EditorView.theme(
    {
      '&': {
        height: '100%',
        backgroundColor: 'transparent',
        color: 'var(--ui-text)',
      },
      '.cm-scroller': {
        overflow: 'auto',
        fontFamily:
          'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
        fontSize: '13px',
        lineHeight: '1.55',
      },
      '&.cm-focused': { outline: 'none' },
      '.cm-gutters': {
        backgroundColor: 'var(--ui-bg-muted)',
        color: 'var(--ui-text-muted)',
        borderRight: '1px solid var(--ui-border)',
      },
      '.cm-activeLine': {
        backgroundColor: dark ? 'rgba(255,255,255,0.045)' : 'rgba(0,0,0,0.04)',
      },
      '.cm-activeLineGutter': {
        backgroundColor: 'transparent',
      },
      '.cm-cursor, .cm-dropCursor': {
        borderLeftColor: 'var(--ui-text)',
      },
      '.cm-selectionBackground, &.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground':
        {
          backgroundColor: dark ? 'rgba(59,130,246,0.35)' : 'rgba(59,130,246,0.22)',
        },
      '.cm-placeholder': {
        color: 'var(--ui-text-muted)',
      },
    },
    { dark },
  )
}

function completionSource(schema) {
  return (context) => {
    const before = context.state.doc.sliceString(0, context.pos)
    if (!isInAction(before)) {
      return null
    }
    const word = context.matchBefore(/[.$A-Za-z0-9_]*/)
    if (!word && !context.explicit) {
      return null
    }
    const options = []
    for (const kw of TEMPLATE_KEYWORDS) {
      options.push({ label: kw, type: 'keyword' })
    }
    for (const fn of TEMPLATE_BUILTINS) {
      options.push({ label: fn, type: 'function' })
    }
    for (const fn of schema.functions ?? []) {
      options.push({
        label: fn.name,
        type: 'function',
        detail: fn.args,
        info: fn.description,
      })
    }
    for (const variable of schema.variables ?? []) {
      options.push({
        label: variable.name,
        type: 'variable',
        detail: variable.type,
        info: variable.description,
      })
    }
    return {
      from: word ? word.from : context.pos,
      options,
      validFor: /^[.$A-Za-z0-9_]*$/,
    }
  }
}

function appearance(dark) {
  return [chromeTheme(dark), syntaxHighlighting(dark ? darkHighlight : lightHighlight)]
}

export function createGoTemplateEditor({
  parent,
  doc,
  schema,
  dark,
  placeholder,
  onChange,
  onApply,
}) {
  const appearanceComp = new Compartment()
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc: doc ?? '',
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        highlightActiveLine(),
        history(),
        EditorView.lineWrapping,
        bracketMatching(),
        closeBrackets(),
        highlightSelectionMatches(),
        autocompletion({ override: [completionSource(schema ?? {})] }),
        Prec.highest(
          keymap.of([
            {
              key: 'Mod-Enter',
              run: () => {
                onApply?.()
                return true
              },
            },
          ]),
        ),
        keymap.of([
          ...closeBracketsKeymap,
          ...completionKeymap,
          ...searchKeymap,
          ...historyKeymap,
          ...defaultKeymap,
          indentWithTab,
        ]),
        goTemplateLanguage,
        appearanceComp.of(appearance(!!dark)),
        placeholder ? placeholderExt(placeholder) : [],
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            onChange?.(update.state.doc.toString())
          }
        }),
      ],
    }),
  })

  return {
    getValue() {
      return view.state.doc.toString()
    },
    setValue(value) {
      const next = value ?? ''
      if (view.state.doc.toString() === next) {
        return
      }
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: next },
      })
    },
    setDark(isDark) {
      view.dispatch({
        effects: appearanceComp.reconfigure(appearance(!!isDark)),
      })
    },
    insert(snippet) {
      const { from, to } = view.state.selection.main
      const before = view.state.doc.sliceString(0, from)
      const text = isInAction(before) ? unwrapActionSnippet(snippet) : snippet
      view.dispatch({
        changes: { from, to, insert: text },
        selection: { anchor: from + text.length },
        scrollIntoView: true,
      })
      view.focus()
    },
    focus() {
      view.focus()
    },
    setCursor(pos) {
      const max = view.state.doc.length
      const anchor = Math.max(0, Math.min(pos, max))
      view.dispatch({ selection: { anchor }, scrollIntoView: true })
    },
    destroy() {
      view.destroy()
    },
  }
}
