import { reactive } from 'vue'

const DARK_MODE_STORAGE_KEY = 'factum:dark-mode'

const storedDarkMode = localStorage.getItem(DARK_MODE_STORAGE_KEY) !== 'false'
if (storedDarkMode) {
  document.documentElement.classList.add('dark')
}

const layoutState = reactive({
  darkTheme: storedDarkMode,
  mobileMenuOpen: false,
})

export function useLayout() {
  const toggleDarkMode = () => {
    if (!document.startViewTransition) {
      executeDarkModeToggle()
      return
    }

    document.startViewTransition(() => executeDarkModeToggle())
  }

  const executeDarkModeToggle = () => {
    layoutState.darkTheme = !layoutState.darkTheme
    document.documentElement.classList.toggle('dark')
    localStorage.setItem(DARK_MODE_STORAGE_KEY, layoutState.darkTheme)
  }

  const toggleMobileMenu = () => {
    layoutState.mobileMenuOpen = !layoutState.mobileMenuOpen
  }

  const closeMobileMenu = () => {
    layoutState.mobileMenuOpen = false
  }

  return {
    layoutState,
    toggleDarkMode,
    toggleMobileMenu,
    closeMobileMenu,
  }
}
