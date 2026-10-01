// Form layout in dialogs (AGENTS.md, GUI design rules): a field's label sits
// beside it when the screen is wide enough, above it on a narrow screen.
// UFormField uses this layout from the Nuxt UI theme in vite.config.js.
// Pass inlineField as :ui when a field must opt in on its own.

// inlineField is the `ui` of a UFormField in a dialog form.
export const inlineField = {
  root: 'sm:grid sm:grid-cols-[11rem_minmax(0,1fr)] sm:gap-x-4',
  labelWrapper: 'flex content-center items-center justify-between gap-1 sm:pt-1.5',
  container: 'relative mt-1 sm:mt-0',
}

// wideModal is the `ui` of a UModal holding such a form.
export const wideModal = { content: 'sm:max-w-2xl' }
