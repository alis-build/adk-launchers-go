/**
 * Snackbar notification store.
 *
 * Provides a queue-based snackbar notification system. Multiple snackbars
 * can be queued and are displayed sequentially by the `App.vue` layout.
 * Convenience methods (`error`, `success`, `warn`, `info`) apply preset
 * colors and icons matching the notification severity.
 *
 * @module store/snackbar
 */
import { defineStore } from 'pinia'
import { v4 as uuidv4 } from 'uuid'
import { ref } from 'vue'

const STORE_ID = 'snackbar'

/**
 * Configuration for a single snackbar notification.
 */
export interface SnackbarOptions {
  /** The message text displayed in the snackbar. */
  text: string
  /** Vuetify color string (e.g. `'error'`, `'success'`, `'#ff0000'`). */
  color?: string
  /** Auto-dismiss delay in milliseconds. Defaults to 4000. */
  timeout?: number
  /** Screen position for the snackbar. Defaults to `'bottom start'`. */
  location?: 'top' | 'bottom' | 'top start' | 'top end' | 'top center' | 'bottom start' | 'bottom end' | 'bottom center'
  /** Vuetify variant styling. Defaults to `'flat'`. */
  variant?: 'text' | 'flat' | 'elevated' | 'tonal' | 'outlined'
  /** Material Symbols icon name shown before the text. */
  icon?: string
  /** Whether the user can manually dismiss the snackbar. Defaults to `true`. */
  closable?: boolean
  /** Unique identifier for the snackbar instance (auto-generated if omitted). */
  id?: string
}

/**
 * Pinia store that manages a FIFO queue of snackbar notifications.
 * The `App.vue` template iterates over `queue` to render active snackbars.
 */
export const useSnackbarStore = defineStore(STORE_ID, () => {
  /** Ordered list of snackbar notifications waiting to be displayed. */
  const queue = ref<SnackbarOptions[]>([])

  /**
   * Enqueues a snackbar notification with the given options.
   * Applies default values for `id`, `timeout`, `location`, `variant`, and `closable`.
   *
   * @param options - Snackbar display configuration.
   */
  const show = (options: SnackbarOptions) => {
    queue.value.push({
      id: uuidv4(),
      timeout: 4000,
      location: 'bottom start',
      variant: 'flat',
      closable: true,
      ...options,
    })
  }

  /**
   * Shows an error snackbar (red, with error icon).
   * @param text - The error message to display.
   * @param options - Optional overrides for other snackbar properties.
   */
  const error = (text: string, options?: Omit<SnackbarOptions, 'text' | 'color'>) => {
    show({
      text,
      color: 'error',
      icon: 'error',
      ...options,
    })
  }

  /**
   * Shows a success snackbar (green, with check_circle icon).
   * @param text - The success message to display.
   * @param options - Optional overrides for other snackbar properties.
   */
  const success = (text: string, options?: Omit<SnackbarOptions, 'text' | 'color'>) => {
    show({
      text,
      color: 'success',
      icon: 'check_circle',
      ...options,
    })
  }

  /**
   * Shows a warning snackbar (amber, with warning icon).
   * @param text - The warning message to display.
   * @param options - Optional overrides for other snackbar properties.
   */
  const warn = (text: string, options?: Omit<SnackbarOptions, 'text' | 'color'>) => {
    show({
      text,
      color: 'warning',
      icon: 'warning',
      ...options,
    })
  }

  /**
   * Shows an informational snackbar (blue, with info icon).
   * @param text - The informational message to display.
   * @param options - Optional overrides for other snackbar properties.
   */
  const info = (text: string, options?: Omit<SnackbarOptions, 'text' | 'color'>) => {
    show({
      text,
      color: 'info',
      icon: 'info',
      ...options,
    })
  }

  /**
   * Removes a snackbar from the queue by its array index.
   * @param index - Zero-based index of the snackbar to remove.
   */
  const remove = (index: number) => {
    queue.value.splice(index, 1)
  }

  /** Removes all snackbars from the queue immediately. */
  const clear = () => {
    queue.value = []
  }

  return {
    queue,
    show,
    error,
    success,
    warn,
    info,
    remove,
    clear,
  }
})

/**
 * Convenience composable that returns only the action methods from the snackbar store,
 * without exposing the internal queue or `remove`/`clear` methods.
 *
 * @returns An object with `error`, `success`, `warn`, `info`, and `show` methods.
 */
export const useSnackbar = () => {
  const store = useSnackbarStore()
  return {
    error: store.error,
    success: store.success,
    warn: store.warn,
    info: store.info,
    show: store.show,
  }
}
