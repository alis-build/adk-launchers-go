/**
 * Clipboard utility.
 * @module utils/clipboard
 */

/**
 * Copies the given text to the user's system clipboard using the
 * Clipboard API (`navigator.clipboard.writeText`).
 *
 * @param text - The string to copy to the clipboard.
 * @returns Resolves when the text has been copied successfully.
 * @throws {Error} If the browser does not support the Clipboard API or the write fails.
 */
export async function copyToClipboard(text: string): Promise<void> {
  if (navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch (error) {
      console.error(error);
    }
  }

  const err = new Error();
  err.message = `Browser does not support copy functionality. Manually copy the text:\n\n ${Error}`;
  throw err;
}
