// STORAGE

// readStored gives back a saved value, or null when there is none or the browser will not let us look.
export function readStored(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

// writeStored saves a value, removes it when it is empty, and stays quiet when the browser says no.
export function writeStored(key: string, value: string): void {
  try {
    if (value === '') {
      window.localStorage.removeItem(key)
    } else {
      window.localStorage.setItem(key, value)
    }
  } catch {
    // Private windows and strict settings can block storage, and the app still works without it.
  }
}
