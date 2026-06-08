/**
 * Gemini-style suggestion chips on the home page. Edit this list to customize starters.
 * Each chip sends a single plain-text user message when clicked.
 */
export interface SuggestionChipDefinition {
  readonly id: string
  /** Short label shown on the chip */
  readonly label: string
  /** Full message text sent to the agent */
  readonly text: string
}

export const SUGGESTION_CHIPS = [
  {
    id: 'what-can-you-do',
    label: 'What can you do?',
    text: 'What can you do?',
  },
  {
    id: 'what-problem-are-you-solving',
    label: 'What problem are you solving?',
    text: 'What problem are you solving?',
  },
  {
    id: 'time',
    label: 'What time is it?',
    text: 'What time is it?',
  },
] as const satisfies readonly SuggestionChipDefinition[]

export type SuggestionChipId = (typeof SUGGESTION_CHIPS)[number]['id']
