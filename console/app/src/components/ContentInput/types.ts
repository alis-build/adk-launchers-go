/**
 * Content input message part types.
 *
 * These classes represent the different types of content a user can
 * compose in the chat input area. Each part carries a discriminated
 * `type` field so consumers can pattern-match on it.
 *
 * Used by the `ContentInput` component, the `ThreadComposerStore`,
 * and the `useStartThreadFromParts` composable.
 *
 * @module components/ContentInput/types
 */

/**
 * Abstract base class for all message part types.
 * Subclasses must specify a discriminant `type` field.
 */
export abstract class MessagePart {
  /** Discriminant tag identifying the concrete part type. */
  abstract readonly type: 'text' | 'file' | 'audio'
}

/**
 * A plain-text (Markdown) message part.
 * Created from the rich-text editor content.
 */
export class TextMessagePart extends MessagePart {
  readonly type = 'text' as const
  /** The markdown-formatted text content. */
  content: string

  /**
   * @param content - Markdown text entered by the user.
   */
  constructor(content: string) {
    super()
    this.content = content
  }
}

/**
 * A file attachment message part.
 * Created from file uploads via drag-and-drop or the file picker.
 */
export class FileMessagePart extends MessagePart {
  readonly type = 'file' as const
  /** The raw File object selected by the user. */
  file: File
  /** MIME type of the file (defaults to `file.type`). */
  mimeType: string
  /** Display name of the file (defaults to `file.name`). */
  fileName: string

  /**
   * @param file - The browser File object.
   * @param mimeType - Override MIME type (defaults to `file.type`).
   * @param fileName - Override file name (defaults to `file.name`).
   */
  constructor(file: File, mimeType?: string, fileName?: string) {
    super()
    this.file = file
    this.mimeType = mimeType ?? file.type
    this.fileName = fileName ?? file.name
  }
}

/**
 * An audio recording message part.
 * Created from the in-browser audio recorder.
 */
export class AudioMessagePart extends MessagePart {
  readonly type = 'audio' as const
  /** The recorded audio data as a Blob. */
  audioBlob: Blob
  /** MIME type of the recording (defaults to `'audio/mp4'`). */
  mimeType: string
  /** Suggested file name for the recording (defaults to `'recording.mp4'`). */
  fileName: string

  /**
   * @param audioBlob - The recorded audio Blob.
   * @param mimeType - Override MIME type (defaults to `'audio/mp4'`).
   * @param fileName - Override file name (defaults to `'recording.mp4'`).
   */
  constructor(audioBlob: Blob, mimeType?: string, fileName?: string) {
    super()
    this.audioBlob = audioBlob
    this.mimeType = mimeType ?? 'audio/mp4'
    this.fileName = fileName ?? 'recording.mp4'
  }
}
