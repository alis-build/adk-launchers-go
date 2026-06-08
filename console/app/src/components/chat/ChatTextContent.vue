<script setup lang="ts">
  import DOMPurify from 'dompurify'
  import hljs from 'highlight.js/lib/core'
  import bash from 'highlight.js/lib/languages/bash'
  import css from 'highlight.js/lib/languages/css'
  import go from 'highlight.js/lib/languages/go'
  import java from 'highlight.js/lib/languages/java'
  import javascript from 'highlight.js/lib/languages/javascript'
  import json from 'highlight.js/lib/languages/json'
  import python from 'highlight.js/lib/languages/python'
  import sql from 'highlight.js/lib/languages/sql'
  import typescript from 'highlight.js/lib/languages/typescript'
  import xml from 'highlight.js/lib/languages/xml'
  import yaml from 'highlight.js/lib/languages/yaml'
  import MarkdownIt from 'markdown-it'
  import markdownItGithubAlerts from 'markdown-it-github-alerts'
  import markdownItHighlightjs from 'markdown-it-highlightjs'
  import { computed, onMounted, onUpdated, useTemplateRef } from 'vue'

  hljs.registerLanguage('javascript', javascript)
  hljs.registerLanguage('js', javascript)
  hljs.registerLanguage('typescript', typescript)
  hljs.registerLanguage('ts', typescript)
  hljs.registerLanguage('python', python)
  hljs.registerLanguage('json', json)
  hljs.registerLanguage('bash', bash)
  hljs.registerLanguage('shell', bash)
  hljs.registerLanguage('sh', bash)
  hljs.registerLanguage('xml', xml)
  hljs.registerLanguage('html', xml)
  hljs.registerLanguage('css', css)
  hljs.registerLanguage('go', go)
  hljs.registerLanguage('yaml', yaml)
  hljs.registerLanguage('yml', yaml)
  hljs.registerLanguage('sql', sql)
  hljs.registerLanguage('java', java)

  const md = new MarkdownIt({
    html: false,
    breaks: true,
    linkify: true,
  })

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  md.use(markdownItHighlightjs as any, { hljs, auto: false, inline: false })
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  md.use(markdownItGithubAlerts as any)

  const defaultFence = md.renderer.rules.fence!.bind(md.renderer.rules)
  md.renderer.rules.fence = (tokens, idx, options, env, self) => {
    const token = tokens[idx]!
    const lang = token.info.trim().split(/\s+/)[0] || 'text'
    const renderedCode = defaultFence(tokens, idx, options, env, self)

    return `<div class="codeblock"><div class="codeblock-header"><span class="codeblock-lang">${md.utils.escapeHtml(lang)}</span><button class="codeblock-copy" type="button"><span class="codeblock-copy-icon">content_copy</span><span class="codeblock-copy-label">Copy</span></button></div>${renderedCode}</div>`
  }

  const props = defineProps<{
    /** Markdown source rendered as sanitized HTML. */
    content: string
  }>()

  const textPartEl = useTemplateRef<HTMLElement>('textPartEl')

  const renderedMarkdown = computed(() => {
    const text = props.content
    if (!text || text.trim().length === 0) {
      return '&nbsp;'
    }

    try {
      const html = md.render(text)
      return DOMPurify.sanitize(html, {
        ADD_TAGS: ['button', 'svg', 'path'],
        ADD_ATTR: ['type', 'viewBox', 'width', 'height', 'aria-hidden', 'd', 'fill', 'class'],
      })
    } catch (e) {
      console.error('[ChatTextContent] markdown render error:', e)
      return text.replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\n/g, '<br>')
    }
  })

  function attachCopyHandlers() {
    if (!textPartEl.value) return
    const buttons = textPartEl.value.querySelectorAll<HTMLButtonElement>('.codeblock-copy')
    buttons.forEach((btn) => {
      if (btn.dataset.bound) return
      btn.dataset.bound = '1'
      btn.addEventListener('click', () => {
        const codeblock = btn.closest('.codeblock')
        const codeEl = codeblock?.querySelector('pre code')
        const code = codeEl?.textContent ?? ''
        navigator.clipboard?.writeText(code)
        const label = btn.querySelector('.codeblock-copy-label')
        const icon = btn.querySelector('.codeblock-copy-icon')
        if (label) label.textContent = 'Copied'
        if (icon) icon.textContent = 'check'
        setTimeout(() => {
          if (label) label.textContent = 'Copy'
          if (icon) icon.textContent = 'content_copy'
        }, 1600)
      })
    })
  }

  onMounted(attachCopyHandlers)
  onUpdated(attachCopyHandlers)
</script>

<template>
  <div
    ref="textPartEl"
    class="chat-text-content"
    v-html="renderedMarkdown"
  />
</template>

<style lang="scss" scoped>
  .chat-text-content {
    line-height: 1.6;
    word-wrap: break-word;
    overflow-wrap: break-word;

    :deep(p) {
      margin: 0 0 0.65em;

      &:last-child {
        margin-bottom: 0;
      }
    }

    :deep(h1) {
      font-size: 1.5em;
      font-weight: 600;
      margin: 1em 0 0.5em;
      color: rgb(var(--v-theme-on-surface));

      &:first-child {
        margin-top: 0;
      }
    }

    :deep(h2) {
      font-size: 1.3em;
      font-weight: 600;
      margin: 0.9em 0 0.4em;
      color: rgb(var(--v-theme-on-surface));

      &:first-child {
        margin-top: 0;
      }
    }

    :deep(h3) {
      font-size: 1.15em;
      font-weight: 600;
      margin: 0.8em 0 0.35em;
      color: rgb(var(--v-theme-on-surface));

      &:first-child {
        margin-top: 0;
      }
    }

    :deep(h4),
    :deep(h5),
    :deep(h6) {
      font-size: 1em;
      font-weight: 600;
      margin: 0.7em 0 0.3em;
      color: rgb(var(--v-theme-on-surface));

      &:first-child {
        margin-top: 0;
      }
    }

    :deep(strong) {
      font-weight: 600;
      color: rgb(var(--v-theme-on-surface));
    }

    :deep(em) {
      font-style: italic;
    }

    :deep(a) {
      color: rgb(var(--v-theme-primary));
      text-decoration: none;
      border-bottom: 1px solid transparent;
      transition: border-color 0.15s ease;

      &:hover {
        border-bottom-color: rgb(var(--v-theme-primary));
      }
    }

    :deep(code:not(.hljs)) {
      font-family: 'JetBrains Mono', ui-monospace, 'SF Mono', Menlo, monospace;
      font-size: 0.85em;
      background-color: rgba(var(--v-theme-on-surface), 0.08);
      padding: 2px 6px;
      border-radius: 4px;
      color: rgb(var(--v-theme-on-surface));
    }

    :deep(.codeblock) {
      background-color: rgba(var(--v-theme-on-surface), 0.04);
      border: 1px solid rgba(var(--v-theme-on-surface), 0.08);
      border-radius: 10px;
      overflow: hidden;
      margin: 0.75em 0;
    }

    :deep(.codeblock-header) {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 6px 12px;
      border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.08);
    }

    :deep(.codeblock-lang) {
      font-family: 'JetBrains Mono', ui-monospace, 'SF Mono', Menlo, monospace;
      font-size: 11px;
      color: rgba(var(--v-theme-on-surface), 0.45);
    }

    :deep(.codeblock-copy) {
      display: flex;
      align-items: center;
      gap: 4px;
      font-size: 11px;
      color: rgba(var(--v-theme-on-surface), 0.45);
      background: transparent;
      border: none;
      padding: 4px 8px;
      border-radius: 5px;
      cursor: pointer;
      font-family: inherit;
      transition: background-color 0.12s ease, color 0.12s ease;

      &:hover {
        background-color: rgba(var(--v-theme-on-surface), 0.08);
        color: rgb(var(--v-theme-on-surface));
      }
    }

    :deep(.codeblock-copy-icon) {
      font-family: 'Material Symbols Outlined', 'Material Icons';
      font-size: 14px;
      font-weight: normal;
      font-style: normal;
      line-height: 1;
      letter-spacing: normal;
      text-transform: none;
      white-space: nowrap;
      word-wrap: normal;
      direction: ltr;
      -webkit-font-feature-settings: 'liga';
      -webkit-font-smoothing: antialiased;
    }

    :deep(.codeblock pre) {
      margin: 0;
      padding: 12px;
      overflow-x: auto;
    }

    :deep(.codeblock pre code) {
      font-family: 'JetBrains Mono', ui-monospace, 'SF Mono', Menlo, monospace;
      font-size: 12px;
      line-height: 1.55;
      color: rgb(var(--v-theme-on-surface));
      background: transparent;
      padding: 0;
      border-radius: 0;
    }

    :deep(pre:not(.codeblock pre)) {
      background-color: rgba(var(--v-theme-on-surface), 0.04);
      border: 1px solid rgba(var(--v-theme-on-surface), 0.08);
      border-radius: 10px;
      padding: 12px;
      overflow-x: auto;
      margin: 0.75em 0;

      code {
        font-family: 'JetBrains Mono', ui-monospace, 'SF Mono', Menlo, monospace;
        font-size: 12px;
        line-height: 1.55;
        background: transparent;
        padding: 0;
        border-radius: 0;
      }
    }

    :deep(ul),
    :deep(ol) {
      margin: 0.5em 0;
      padding-left: 1.5em;
    }

    :deep(li) {
      margin: 0.2em 0;
      line-height: 1.55;

      p {
        margin: 0;
      }
    }

    :deep(blockquote) {
      border-left: 3px solid rgba(var(--v-theme-on-surface), 0.2);
      padding: 0.25em 0 0.25em 1em;
      margin: 0.75em 0;
      color: rgba(var(--v-theme-on-surface), 0.7);

      p:last-child {
        margin-bottom: 0;
      }
    }

    :deep(.markdown-alert) {
      border-left: 3px solid;
      border-radius: 8px;
      padding: 10px 14px;
      margin: 0.75em 0;
    }

    :deep(.markdown-alert-title) {
      font-size: 13px;
      font-weight: 600;
      display: flex;
      align-items: center;
      gap: 6px;
      margin-bottom: 4px;

      svg,
      .octicon {
        flex-shrink: 0;
      }
    }

    :deep(.markdown-alert-note) {
      background-color: rgba(var(--v-theme-info), 0.08);
      border-color: rgb(var(--v-theme-info));

      .markdown-alert-title {
        color: rgb(var(--v-theme-info));
      }
    }

    :deep(.markdown-alert-tip) {
      background-color: rgba(var(--v-theme-success), 0.08);
      border-color: rgb(var(--v-theme-success));

      .markdown-alert-title {
        color: rgb(var(--v-theme-success));
      }
    }

    :deep(.markdown-alert-important) {
      background-color: rgba(var(--v-theme-primary), 0.08);
      border-color: rgb(var(--v-theme-primary));

      .markdown-alert-title {
        color: rgb(var(--v-theme-primary));
      }
    }

    :deep(.markdown-alert-warning) {
      background-color: rgba(var(--v-theme-warning), 0.08);
      border-color: rgb(var(--v-theme-warning));

      .markdown-alert-title {
        color: rgb(var(--v-theme-warning));
      }
    }

    :deep(.markdown-alert-caution) {
      background-color: rgba(var(--v-theme-error), 0.08);
      border-color: rgb(var(--v-theme-error));

      .markdown-alert-title {
        color: rgb(var(--v-theme-error));
      }
    }

    :deep(table) {
      border-collapse: collapse;
      width: 100%;
      margin: 0.75em 0;
      font-size: 0.9em;
    }

    :deep(th) {
      background-color: rgba(var(--v-theme-on-surface), 0.06);
      font-weight: 600;
      text-align: left;
      padding: 8px 12px;
      border: 1px solid rgba(var(--v-theme-on-surface), 0.1);
    }

    :deep(td) {
      padding: 8px 12px;
      border: 1px solid rgba(var(--v-theme-on-surface), 0.1);
    }

    :deep(tr:nth-child(even)) {
      background-color: rgba(var(--v-theme-on-surface), 0.02);
    }

    :deep(hr) {
      border: none;
      border-top: 1px solid rgba(var(--v-theme-on-surface), 0.12);
      margin: 1em 0;
    }

    :deep(img) {
      max-width: 100%;
      height: auto;
      border-radius: 8px;
    }

    :deep(.hljs-keyword),
    :deep(.hljs-selector-tag),
    :deep(.hljs-title),
    :deep(.hljs-section) {
      color: rgb(var(--v-theme-primary));
    }

    :deep(.hljs-string),
    :deep(.hljs-addition) {
      color: rgb(var(--v-theme-success));
    }

    :deep(.hljs-number),
    :deep(.hljs-literal) {
      color: rgb(var(--v-theme-warning));
    }

    :deep(.hljs-comment),
    :deep(.hljs-quote) {
      color: rgba(var(--v-theme-on-surface), 0.4);
      font-style: italic;
    }

    :deep(.hljs-deletion) {
      color: rgb(var(--v-theme-error));
    }

    :deep(.hljs-meta),
    :deep(.hljs-doctag) {
      color: rgba(var(--v-theme-on-surface), 0.6);
    }

    :deep(.hljs-attr),
    :deep(.hljs-attribute) {
      color: rgb(var(--v-theme-info));
    }

    :deep(.hljs-built_in),
    :deep(.hljs-type),
    :deep(.hljs-class) {
      color: rgb(var(--v-theme-warning));
    }

    :deep(.hljs-params) {
      color: rgba(var(--v-theme-on-surface), 0.8);
    }

    :deep(.hljs-symbol),
    :deep(.hljs-bullet) {
      color: rgb(var(--v-theme-info));
    }

    :deep(.hljs-variable),
    :deep(.hljs-template-variable) {
      color: rgb(var(--v-theme-error));
    }

    :deep(.hljs-name),
    :deep(.hljs-selector-id),
    :deep(.hljs-selector-class) {
      color: rgb(var(--v-theme-primary));
    }
  }
</style>
