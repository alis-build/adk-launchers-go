/**
 * Global application store.
 *
 * Manages top-level UI state (navigation drawer, global loading indicator)
 * and the currently authenticated user. The user is fetched from the
 * `/auth/me` endpoint provided by the console backend's auth middleware.
 *
 * @module store/app
 */
import { defineStore } from 'pinia'
import { computed, ref, type Ref } from 'vue'

const STORE_ID = 'app'

/**
 * Minimal user profile returned by the `/auth/me` endpoint.
 * `sub` is the OIDC subject identifier; `email` is the user's email address.
 */
type AuthenticatedUser = {
  /** OIDC subject identifier. */
  sub?: string
  /** User's email address. */
  email?: string
}

/**
 * Derives a human-readable display name from an email address.
 * Splits the local part on `.`, `_`, and `-`, then title-cases each segment.
 *
 * @param email - The user's email address.
 * @returns A title-cased display name, or `'User'` if the email is empty/missing.
 *
 * @example
 * formatDisplayName('john.doe@example.com') // 'John Doe'
 */
function formatDisplayName(email?: string): string {
  const localPart = email?.split('@')[0]?.trim()
  if (!localPart) return 'User'

  const parts = localPart
    .split(/[._-]+/)
    .map((part) => part.trim())
    .filter(Boolean)

  if (parts.length === 0) return 'User'

  return parts.map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(' ')
}

/**
 * Pinia store for global application state.
 *
 * Exposes reactive properties for:
 * - Navigation drawer visibility (open/close/toggle).
 * - A global loading flag used while thread data is being fetched.
 * - Authenticated user info (retrieved from `/auth/me`).
 * - Computed display values derived from the user profile (display name, initials, email).
 */
export const useAppStore = defineStore(STORE_ID, () => {
  /** Whether the left navigation drawer is currently open. */
  const navigationDrawerOpen = ref(true)
  /** Global loading flag shown in the top bar while a thread is being loaded. */
  const globalThreadLoading = ref(false)

  /** Toggles the navigation drawer between open and closed. */
  const toggleNavigationDrawer = () => {
    navigationDrawerOpen.value = !navigationDrawerOpen.value
  }

  /** Opens the navigation drawer. */
  const openNavigationDrawer = () => {
    navigationDrawerOpen.value = true
  }

  /** Closes the navigation drawer. */
  const closeNavigationDrawer = () => {
    navigationDrawerOpen.value = false
  }

  /**
   * Sets the navigation drawer open state to an explicit value.
   * @param value - `true` to open, `false` to close.
   */
  const setNavigationDrawerOpen = (value: boolean) => {
    navigationDrawerOpen.value = value
  }

  /**
   * Sets the global thread loading state.
   * @param value - `true` when a thread is being loaded, `false` when complete.
   */
  const setGlobalThreadLoading = (value: boolean) => {
    globalThreadLoading.value = value
  }

  /** The currently authenticated user, or `undefined` if not yet fetched. */
  const user: Ref<AuthenticatedUser | undefined> = ref(undefined)

  /**
   * Fetches the current user profile from `/auth/me` and stores it.
   * The endpoint is served by the console backend's auth middleware
   * and returns OIDC claims (subject, email).
   *
   * @throws {Error} If the HTTP response is not OK or the network request fails.
   */
  const retrieveMyUser = async () => {
    try {
      const resp = await fetch('/auth/me', {
        credentials: 'same-origin',
      })
      if (!resp.ok) {
        throw new Error(`Failed to retrieve user: ${resp.status} ${resp.statusText}`)
      }
      user.value = (await resp.json()) as AuthenticatedUser
    } catch (error) {
      console.error(error)
      throw error
    }
  }

  /** Title-cased display name derived from the user's email (e.g. "John Doe"). */
  const userDisplayName = computed(() => {
    return formatDisplayName(user.value?.email)
  })

  /** One or two uppercase initials for the user avatar (e.g. "JD"). */
  const userInitials = computed(() => {
    const parts = userDisplayName.value.split(' ').filter(Boolean)
    if (parts.length >= 2) {
      return `${parts[0][0]}${parts[1][0]}`.toUpperCase()
    }
    if (parts.length === 1) {
      return parts[0][0]?.toUpperCase() || 'U'
    }
    return 'U'
  })

  /** The user's email address, or empty string if unavailable. */
  const userEmail = computed(() => user.value?.email || '')

  /** Placeholder for user profile picture URL (not yet implemented). */
  const userProfilePicture = computed(() => '')

  return {
    navigationDrawerOpen,
    toggleNavigationDrawer,
    openNavigationDrawer,
    closeNavigationDrawer,
    setNavigationDrawerOpen,
    globalThreadLoading,
    setGlobalThreadLoading,
    // User
    user,
    retrieveMyUser,
    userDisplayName,
    userInitials,
    userEmail,
    userProfilePicture,
  }
})
