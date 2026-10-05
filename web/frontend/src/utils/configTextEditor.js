import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { bracketMatching } from '@codemirror/language'
import { highlightSelectionMatches, searchKeymap } from '@codemirror/search'
import { Compartment, EditorState, StateEffect, StateField } from '@codemirror/state'
import {
  Decoration,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
} from '@codemirror/view'
import { changedLineNumbers } from './configText'

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
      '.cm-activeLineGutter': { backgroundColor: 'transparent' },
      '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--ui-text)' },
      '.cm-selectionBackground, &.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground':
        {
          backgroundColor: dark ? 'rgba(59,130,246,0.35)' : 'rgba(59,130,246,0.22)',
        },
      '.cm-cfg-changed': {
        backgroundColor: dark ? 'rgba(234, 179, 8, 0.18)' : 'rgba(234, 179, 8, 0.28)',
        boxShadow: 'inset 3px 0 0 #ca8a04',
      },
    },
    { dark },
  )
}

const refreshMarks = StateEffect.define()

export function createConfigTextEditor({ parent, doc, original, dark, onChange }) {
  let base = original ?? ''
  const appearanceComp = new Compartment()

  function marks(state) {
    const ranges = []
    for (const n of changedLineNumbers(base, state.doc.toString())) {
      if (n < 1 || n > state.doc.lines) continue
      const line = state.doc.line(n)
      ranges.push(Decoration.line({ class: 'cm-cfg-changed' }).range(line.from))
    }
    return Decoration.set(ranges, true)
  }

  const markField = StateField.define({
    create(state) {
      return marks(state)
    },
    update(value, tr) {
      if (tr.docChanged || tr.effects.some((e) => e.is(refreshMarks))) return marks(tr.state)
      return value.map(tr.changes)
    },
    provide: (field) => EditorView.decorations.from(field),
  })

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
        highlightSelectionMatches(),
        keymap.of([...searchKeymap, ...historyKeymap, ...defaultKeymap, indentWithTab]),
        appearanceComp.of(chromeTheme(!!dark)),
        markField,
        EditorView.updateListener.of((update) => {
          if (update.docChanged) onChange?.(update.state.doc.toString())
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
      if (view.state.doc.toString() === next) return
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: next } })
    },
    setOriginal(value) {
      base = value ?? ''
      view.dispatch({ effects: refreshMarks.of(null) })
    },
    setDark(isDark) {
      view.dispatch({ effects: appearanceComp.reconfigure(chromeTheme(!!isDark)) })
    },
    focus() {
      view.focus()
    },
    destroy() {
      view.destroy()
    },
  }
}
