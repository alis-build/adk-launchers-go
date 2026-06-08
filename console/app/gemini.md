# Vue.js Project Guidelines

This document outlines the coding standards and best practices to be followed in this Vue.js project. Adhering to these guidelines ensures consistency, readability, and maintainability of the codebase.

## 1. Composition API Philosophy

Components should be organized by logical concern, not by option type. Group related reactive state, methods, computed properties, and watchers together. This approach, central to the Composition API, makes components easier to read and maintain by co-locating related code.

**Example:**

```typescript
import { ref, computed, onMounted, type Ref } from 'vue'

// Define a type for the user object for better type safety
interface User {
  id: number
  name: string
  // ... other properties
}

// --- LOGICAL CONCERN 1: User Management ---
const users: Ref<User[]> = ref([])
const searchQuery: Ref<string> = ref('')
const loadingFetchUsers: Ref<boolean> = ref(false)
const errorFetchUsers: Ref<string | undefined> = ref(undefined)

const filteredUsers = computed(() => {
  if (!searchQuery.value) return users.value
  return users.value.filter((user) => user.name.toLowerCase().includes(searchQuery.value.toLowerCase()))
})

async function fetchUsers() {
  loadingFetchUsers.value = true
  errorFetchUsers.value = undefined
  try {
    const response = await fetch('https://jsonplaceholder.typicode.com/users')
    if (!response.ok) throw new Error('Failed to fetch users.')
    users.value = await response.json()
  } catch (e) {
    errorFetchUsers.value = (e as Error).message
  } finally {
    loadingFetchUsers.value = false
  }
}

onMounted(fetchUsers)

// --- LOGICAL CONCERN 2: Theme Management ---
// All code related to the theme is self-contained here. It is
// cleanly separated from the user management logic above.
const theme: Ref<'light' | 'dark'> = ref('light')

const toggleTheme = () => {
  theme.value = theme.value === 'light' ? 'dark' : 'light'
}
```

## 2. API Call State Management

Every asynchronous operation, such as an API call, must have dedicated reactive variables to track its loading and error states. This provides a consistent pattern for handling UI feedback during data fetching.

### Loading State

A loading variable must be created for each API call.

- **Naming Convention:** The variable must be prefixed with `loading`, followed by the capitalized name of the function it corresponds to (e.g., `loadingRetrieveProduct`).

- **Implementation:** This ref should be set to `true` before the API call is initiated and `false` in a `finally` block to ensure it is reset whether the call succeeds or fails.

**Example:**

```typescript
import { ref, type Ref } from 'vue'

const loadingRetrieveProduct: Ref<boolean> = ref(false)

async function retrieveProduct(productId: string) {
  loadingRetrieveProduct.value = true
  try {
    // ... API call logic
  } catch (e) {
    // ... error handling
  } finally {
    loadingRetrieveProduct.value = false
  }
}
```

### Error State

An error variable must be created for each API call.

- **Naming Convention:** The variable must be prefixed with `error`, followed by the capitalized name of the function it corresponds to (e.g., `errorRetrieveProduct`).

- **Implementation:** The error ref should be cleared (set to `undefined`) at the start of the API call and set to the error message string in the `catch` block.

**Example:**

```typescript
import { ref, type Ref } from 'vue'

const errorRetrieveProduct: Ref<string | undefined> = ref(undefined)

async function retrieveProduct(productId: string) {
  errorRetrieveProduct.value = undefined
  try {
    // ... API call logic
  } catch (e) {
    errorRetrieveProduct.value = (e as Error).message || 'An unknown error occurred.'
  }
}
```

## 3. Immutability of Reactive Declarations

All reactive variables (`ref`, `reactive`), computed properties (`computed`), and component `props` must be declared using `const`. This prevents accidental reassignment of the reactive reference itself, which is a common source of bugs. You should only ever be mutating the `.value` property of a `ref` or properties of a `reactive` object.

**Example:**

```typescript
import { ref, computed, type Ref } from 'vue'

// GOOD
const count: Ref<number> = ref(0)
const doubled = computed(() => count.value * 2)

// BAD - Avoid using 'let'
// let count = ref(0);
```

## 4. Explicit Typing for Refs

Always explicitly declare the type of a `ref` using the generic `Ref<>` syntax from Vue. This improves type safety and makes the intended data structure clear at a glance.

**Example:**

```typescript
import { ref, type Ref } from 'vue'

interface User {
  /* ... */
}

// GOOD
const year: Ref<string | number> = ref('2024')
const user: Ref<User | undefined> = ref(undefined)
const items: Ref<string[]> = ref([])

// BAD - Avoid relying solely on type inference for refs
// const year = ref('2024');
```

## 5. Use `undefined` instead of `null`

Use `undefined` to represent the absence of a value. This creates a single, consistent way to check for empty or uninitialized states throughout the application, reducing ambiguity between `null` and `undefined`.

**Example:**

```typescript
import { ref, type Ref } from 'vue'

interface User {
  /* ... */
}

// GOOD
const user: Ref<User | undefined> = ref(undefined)

if (user.value === undefined) {
  console.log('User not loaded')
}

// BAD - Avoid using 'null'
// const user: Ref<User | null> = ref(null);
```

## 6. Type-based Prop Declarations

When using `<script setup>`, props should be declared with `defineProps` using a pure type annotation via a generic type argument. This provides full static typing and IDE support, making components more robust and easier to reason about.

**Example:**

```vue
<script setup lang="ts">
  const props = defineProps<{
    foo: string
    bar?: number
  }>()
</script>
```

## 7. Type-based Emit Declarations

Similarly to props, component emits should be declared with `defineEmits` using a type literal in a generic argument. This ensures that all emitted events are statically typed, providing autocompletion and compile-time validation of event names and their payloads.

**Example:**

```vue
<script setup lang="ts">
  // type-based
  const emit = defineEmits<{
    (e: 'change', id: number): void
    (e: 'update', value: string): void
  }>()
</script>
```

## 8. Prioritize Vuetify Utility Classes

Always use Vuetify's built-in utility classes for layout, spacing, and styling whenever possible, rather than defining your own custom styles. This promotes consistency, reduces CSS bundle size, and speeds up development.

### Good Example

<!-- GOOD: Using Vuetify utility classes for spacing, layout, and borders -->

```vue
<template>
  <v-card class="mx-4 pa-2 rounded-xl">
    <div class="d-flex justify-space-between align-center">
      <span>Card Content</span>
      <v-btn>Action</v--btn>
    </div>
  </v-card>
</template>
```

### Bad Example

<!-- BAD: Using inline styles and custom classes where utilities exist -->

```vue
<template>
  <v-card
    class="custom-card"
    style="border-radius: 24px;"
  >
    <div class="custom-flex-container">
      <span>Card Content</span>
      <v-btn>Action</v-btn>
    </div>
  </v-card>
</template>

<style scoped>
  .custom-card {
    margin-left: 16px;
    margin-right: 16px;
    padding: 8px;
  }
  .custom-flex-container {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
</style>
```

## 9. Leverage `vuetify.ts` Configuration

Be aware of the global configurations set in `vuetify.ts`. This includes component aliases and default props. Utilizing these configurations makes component templates cleaner and more maintainable.

### Context: `vuetify.ts`

Assume your `vuetify.ts` file contains the following configurations:

```typescript
// plugins/vuetify.ts
import { createVuetify } from 'vuetify'
import { VBtn } from 'vuetify/components/VBtn'

export default createVuetify({
  // 1. Set global default properties
  defaults: {
    VTextField: {
      density: 'compact',
      variant: 'outlined',
    },
    VBtn: {
      color: 'primary',
    },
  },
  // 2. Create aliases for common component variations
  aliases: {
    VBtnIcon: VBtn,
    VCardOutlined: VCard,
  },
  // ... other configs
})
```

### Good Example

<!-- GOOD: Leverages aliases and avoids redundant default props -->

```vue
<template>
  <div>
    <!-- Uses the alias and doesn't repeat variant="outlined" -->
    <v-card-outlined>
      <!-- Omits density="compact" and variant="outlined" as they are defaults -->
      <v-text-field label="Name" />
    </v-card-outlined>

    <!-- Uses the alias for an icon button -->
    <v-btn-icon icon="mdi-heart" />
  </div>
</template>
```

### Bad Example

<!-- BAD: Ignores aliases and repeats default properties -->

```vue
<template>
  <div>
    <!-- Does not use the VCardOutlined alias -->
    <v-card
      variant="outlined"
      style="border=1px grey solid"
    >
      <!-- Unnecessarily repeats default props set in vuetify.ts -->
      <v-text-field
        label="Name"
        density="compact"
        variant="outlined"
      />
    </v-card>

    <!-- Verbose way to create an icon button -->
    <v-btn icon="mdi-heart" />
  </div>
</template>
```

## 10. Usage of Vuetify Grid System (v-container, v-row, v-col)

This rule outlines the standardized usage of Vuetify's core grid components: v-container, v-row, and v-col. Adhering to these guidelines ensures consistent, responsive, and maintainable layouts across the application.

### Core Philosophy

The Vuetify grid system is designed for macro-level layout structure, not for micro-level component styling. Use it to arrange major content blocks on a page. For arranging smaller elements within a component (e.g., an icon next to text, a button in a form field), prefer CSS Flexbox, utility classes, or other purpose-built components.
The fundamental hierarchy that must be respected is: v-container -> v-row -> v-col

#### 1. The v-container Component

The v-container is the top-level wrapper for your page's grid content. It provides padding and can constrain the content width on large screens.

- When to Use v-container:
  - As the root element for a page's main content area.
    To center and apply a maximum width to your layout (default behavior).
    For a full-width layout using the fluid prop: <v-container fluid>.
- When NOT to Use v-container:
  - Never nest a v-container inside another v-container.
  - Never place a v-container inside a v-row or v-col.
  - To wrap a single, small component like a v-btn.

2. The v-row Component
   The v-row is a horizontal container for columns. It uses a negative margin to counteract the padding from its child v-col elements.

- When to Use v-row:
  - As a direct child of a v-container or a v-col.
  - Whenever you need to place one or more v-col components side-by-side.
- When NOT to Use v-row:
  - As a direct child of another v-row. Nest grids using v-row -> v-col -> v-row.
  - To simply add vertical margin. Use spacing utility classes instead (e.g., class="my-4").

3. The v-col Component
   The v-col is the fundamental content holder of the grid system. Your components and content should almost always be placed inside a v-col.

- When to Use v-col:
  - As a direct child of a v-row. This is a strict rule.
  - To contain your actual content: v-card, v-form, text, etc.
  - To define responsive breakpoints (cols, sm, md, lg, xl props).
- When NOT to Use v-col:
  - Directly inside a v-container.
  - When a simple <div class="d-flex"> would be more efficient for non-columnar layouts.

### Examples

#### Correct Usage

This example shows a standard two-column layout that collapses to a single column on small screens.

```vue
<template>
  <v-container>
    <!-- First row for a page title -->
    <v-row>
      <v-col cols="12">
        <h1>Page Title</h1>
      </v-col>
    </v-row>

    <!-- Second row for the main content -->
    <v-row>
      <!-- Left Column: Main content -->
      <v-col
        cols="12"
        md="8"
      >
        <v-card>
          <v-card-text> Main content goes here. This column takes up 12 units on small screens and 8 on medium screens and up. </v-card-text>
        </v-card>
      </v-col>

      <!-- Right Column: Sidebar -->
      <v-col
        cols="12"
        md="4"
      >
        <v-card>
          <v-card-text> Sidebar content. This column takes up 12 units on small screens and 4 on medium screens and up. </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>
```

#### Incorrect Usage

This example illustrates several common mistakes.

```vue
<template>
  <!-- MISTAKE 1: Nesting a container inside another container -->
  <v-container>
    <v-container>
      <!-- This adds double padding and is incorrect. -->

      <!-- MISTAKE 2: A v-col should not be a direct child of v-container. -->
      <v-col cols="12"> This will not be aligned properly. </v-col>

      <v-row>
        <!-- MISTAKE 3: Using the grid system for a tiny element like a button. -->
        <!-- This is overkill. A div with d-flex or utility classes would be better. -->
        <v-col cols="1">
          <v-btn>OK</v-btn>
        </v-col>
      </v-row>
    </v-container>
  </v-container>
</template>
```

## Standardized Dialog Actions

To ensure a consistent and predictable user experience, action buttons within dialogs (v-dialog) must follow a standard pattern for alignment, order, and styling.

### Core Principles

- **Alignment**: Actions must be right-aligned within the v-card-actions section.
- **Quantity**: Limit actions to two buttons whenever possible to avoid user confusion.
- **Order**: The affirmative (primary) action (e.g., Confirm, Create, Delete) is always on the far right. The dismissive (secondary) action (e.g., Cancel, Close) is to the left of it.
- **Styling**:
  - In almost all cases, all action buttons should be text buttons (variant="text").
  - By default, both buttons should use the primary color (color="primary").
  - Using other colors (e.g., error or warning) should be reserved for critical, irreversible actions like permanent deletion.
  - **Exception**: This standard does not apply to dialogs with multi-step flows (e.g., wizards with "Back" and "Next" buttons), which have their own distinct UX patterns.

### Good Example (Standard Confirmation)

```vue
<template>
  <v-dialog
    v-model="dialog"
    max-width="500px"
  >
    <v-card>
      <v-card-title>Confirm Action</v-card-title>
      <v-card-text> Are you sure you want to proceed with this action? </v-card-text>
      <v-card-actions>
        <!-- Spacer pushes buttons to the right -->
        <v-spacer></v-spacer>
        <!-- Secondary action on the left -->
        <v-btn
          color="primary"
          variant="text"
          @click="dialog = false"
        >
          Cancel
        </v-btn>
        <!-- Primary action on the far right -->
        <v-btn
          color="primary"
          variant="text"
          @click="confirmAction"
        >
          Agree
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
```

### Good Example (Destructive Action)

```vue
<template>
  <v-dialog
    v-model="dialog"
    max-width="500px"
  >
    <v-card>
      <v-card-title>Delete Item</v-card-title>
      <v-card-text> This action cannot be undone. Are you sure you want to permanently delete this item? </v-card-text>
      <v-card-actions>
        <v-spacer></v-spacer>
        <v-btn
          color="primary"
          variant="text"
          @click="dialog = false"
        >
          Cancel
        </v-btn>

        <!-- Destructive action uses a warning color -->

        <v-btn
          color="error"
          variant="text"
          @click="deleteItem"
        >
          Delete
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
```

### Bad Example

```vue
<template>
  <v-dialog
    v-model="dialog"
    max-width="500px"
  >
    <v-card>
      <v-card-title>Confirm Action</v-card-title>
      <v-card-text>...</v-card-text>

      <!-- MISTAKE 1: No spacer, buttons are left-aligned -->
      <v-card-actions>
        <!-- MISTAKE 2: Incorrect order (primary action is not on the far right) -->
        <v-btn
          color="primary"
          variant="tonal"
          @click="confirmAction"
        >
          Agree
        </v-btn>
        <!-- MISTAKE 3: Inconsistent styling (tonal vs text) and unnecessary color -->
        <v-btn
          color="grey"
          variant="text"
          @click="dialog = false"
        >
          Cancel
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
```

## 11. Standard Layout Variations

This section details the two primary layout patterns used in our application to ensure a consistent user experience. These layouts are designed to be responsive and adapt to different screen sizes.

### 11.1. Split Navigation Layout

This layout is the standard for most application views. It consists of a side navigation pane (`<x-navigation-pane>`) and a main content area (`<x-main-card-container>`).

**Key Components:**

- **`<x-navigation-pane>`**: Holds navigation links, action buttons (FABs), and secondary information.
- **`<x-main-card-container>`**: Contains the main view content.
- **`useAppPanelCardHeight`**: A composable that ensures the layout takes up the correct height, respecting the app shell's constraints.

**Implementation Guide:**

```vue
<script setup lang="ts">
  import { useDisplay } from 'vuetify'
  import { useAppPanelCardHeight } from '@/utils/sizing'
  import { useAppStore } from '@/stores/app'

  // 1. Initialize the app store to access navigation state (isNavCollapsed)
  const app = useAppStore()

  // 2. Use display helpers for responsive logic
  const { mdAndUp } = useDisplay()

  // 3. Use the sizing utility to bind height references
  const { appPanelCardRef, appPanelCardHeight } = useAppPanelCardHeight()
</script>

<template>
  <!-- 4. Flex container to hold the pane and main card -->
  <div class="h-100 d-flex justify-center">
    <!-- 5. Navigation Pane -->
    <x-navigation-pane
      :class="{ 'is-collapsed': app.isNavCollapsed }"
      :height="appPanelCardHeight + (mdAndUp ? 0 : 48)"
    >
      <!-- FAB Action -->
      <x-navigation-pane-fab
        prepend-icon="add"
        @click="doSomething"
      >
        Action
      </x-navigation-pane-fab>

      <!-- Navigation Links -->
      <x-list-item
        to="some-route"
        prepend-icon="home"
        active
      >
        Home
      </x-list-item>
    </x-navigation-pane>

    <!-- 6. Main Content Container -->
    <x-main-card-container
      ref="appPanelCardRef"
      :height="appPanelCardHeight + (mdAndUp ? 0 : 48)"
    >
      <!-- Title Bar -->
      <x-main-title class="border-b-thin"> Page Title </x-main-title>

      <!-- View Content -->
      <router-view />
    </x-main-card-container>
  </div>
</template>
```

### 11.3. Main Title Patterns

To maintain a clean separation of concerns while keeping the layout consistent, we often use the `<Teleport>` component. This allows child views to inject content (titles, actions, tabs) into the parent layout's title bar (`x-main-title`), which is typically defined in the navigation layout (Split Navigation Layout).

#### 11.3.1. Parent Component (Skeleton)

The parent component (e.g., `IdeateHome.vue`) defines the `x-main-title` and provides target elements with unique IDs for the child components to target.

**Example:**

```vue
<template>
  <x-main-card-container ...>
    <x-main-title class="border-b-thin pb-0">
      <div class="d-flex justify-space-between align-center">
        <!-- Title Target -->
        <div
          id="view-home-title"
          class="mb-2"
        ></div>

        <!-- Search Target -->
        <div id="view-home-search"></div>

        <div class="d-flex align-center">
          <!-- Actions Target -->
          <div id="view-home-actions"></div>

          <!-- Shared Parent Actions (e.g. Account Selector) -->
          <AccountSelector />
        </div>
      </div>
      <!-- Tabs Target -->
      <div id="view-home-tabs"></div>
    </x-main-title>
    <router-view />
  </x-main-card-container>
</template>
```

#### 11.3.2. Child Component (Injector)

The child component (e.g., `IdeateExplore.vue`) uses `<Teleport>` to render its specific title, actions, or tabs into the parent's defined slots. Note the use of `defer` prop on Teleport to ensure the target exists.

**Example:**

```vue
<template>
  <!-- Inject Title -->
  <Teleport
    defer
    to="#view-home-title"
  >
    <div>My Page Title</div>
  </Teleport>

  <!-- Inject Tabs -->
  <Teleport
    defer
    to="#view-home-tabs"
  >
    <v-tabs v-model="tab">
      <v-tab value="1">Tab 1</v-tab>
      <v-tab value="2">Tab 2</v-tab>
    </v-tabs>
  </Teleport>

  <!-- Inject Actions -->
  <Teleport
    defer
    to="#view-home-actions"
  >
    <v-btn-icon icon="refresh">Refresh</v-btn-icon>
  </Teleport>

  <!-- Main Content -->
  <div class="d-flex w-100 h-100">
    <!-- ... -->
  </div>
</template>
```
