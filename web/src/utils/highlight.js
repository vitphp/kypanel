// 代码语法高亮 —— 基于 highlight.js（与 VSCode 同级的 TextMate 语法引擎）
// 190+ 语言、PHP+HTML 混排、无正则灾难性回溯，彻底替代手写正则版本。
// 通过 classPrefix:'hl-' 保持输出类名与 CodeEditor.vue 的 .ce-hl .hl-* 样式一致。

import hljs from 'highlight.js/lib/core'

// ---- 按需注册常用语言（控制包体积） ----
import javascript  from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import python      from 'highlight.js/lib/languages/python'
import go          from 'highlight.js/lib/languages/go'
import java        from 'highlight.js/lib/languages/java'
import c           from 'highlight.js/lib/languages/c'
import cpp         from 'highlight.js/lib/languages/cpp'
import csharp      from 'highlight.js/lib/languages/csharp'
import xml         from 'highlight.js/lib/languages/xml'
import php         from 'highlight.js/lib/languages/php'
import phpTemplate from 'highlight.js/lib/languages/php-template'
import ruby        from 'highlight.js/lib/languages/ruby'
import rust        from 'highlight.js/lib/languages/rust'
import swift       from 'highlight.js/lib/languages/swift'
import kotlin      from 'highlight.js/lib/languages/kotlin'
import bash        from 'highlight.js/lib/languages/bash'
import css         from 'highlight.js/lib/languages/css'
import scss        from 'highlight.js/lib/languages/scss'
import less        from 'highlight.js/lib/languages/less'
import json        from 'highlight.js/lib/languages/json'
import sql         from 'highlight.js/lib/languages/sql'
import yaml        from 'highlight.js/lib/languages/yaml'
import markdown    from 'highlight.js/lib/languages/markdown'
import ini         from 'highlight.js/lib/languages/ini'
import dockerfile  from 'highlight.js/lib/languages/dockerfile'
import lua         from 'highlight.js/lib/languages/lua'
import perl        from 'highlight.js/lib/languages/perl'

// 注意注册顺序：php-template 依赖 xml + php
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('php', php)
hljs.registerLanguage('php-template', phpTemplate)
for (const [name, mod] of Object.entries({
  javascript, typescript, python, go, java, c, cpp, csharp,
  ruby, rust, swift, kotlin, bash, css, scss, less, json, sql,
  yaml, markdown, ini, dockerfile, lua, perl
})) hljs.registerLanguage(name, mod)

// 输出 span 类名使用 hl- 前缀（默认是 hljs-），与 .ce-hl .hl-* 样式配套
hljs.configure({ classPrefix: 'hl-' })

// 转义 HTML（语言不可用时的兜底）
function esc(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

// 面板内部语言名 → highlight.js 语言名
const HL_LANG = {
  js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'javascript',
  ts: 'typescript', tsx: 'typescript',
  py: 'python', go: 'go', java: 'java', c: 'c', h: 'c', cpp: 'cpp', cc: 'cpp',
  hpp: 'cpp', cs: 'csharp', php: 'php', rb: 'ruby', rs: 'rust', swift: 'swift',
  kt: 'kotlin', sh: 'bash', bash: 'bash', zsh: 'bash', html: 'xml', htm: 'xml',
  css: 'css', scss: 'scss', less: 'less', json: 'json', xml: 'xml', svg: 'xml',
  vue: 'xml', sql: 'sql', yaml: 'yaml', yml: 'yaml', md: 'markdown',
  conf: 'ini', ini: 'ini', toml: 'ini', dockerfile: 'dockerfile',
  lua: 'lua', pl: 'perl', pm: 'perl'
}

// 按扩展名返回语言（面板内部语言名，兼容 CodeEditor/FileManager 已有逻辑）
export function langOf(filename) {
  const ext = (filename.split('.').pop() || '').toLowerCase()
  if (filename === 'Dockerfile' || /dockerfile/i.test(filename)) return 'dockerfile'
  // 各类锁文件：JSON 系的（package-lock / composer.lock）按 JSON 高亮；
  // yarn.lock / Gemfile.lock / poetry.lock 等按对应语言/文本处理
  if (ext === 'lock') {
    const lower = filename.toLowerCase()
    if (lower === 'composer.lock' || lower === 'package-lock.json' || lower.endsWith('-lock.json')) return 'json'
    if (lower === 'yarn.lock') return 'yaml'
    if (lower === 'gemfile.lock') return 'ruby'
    if (lower === 'poetry.lock') return 'toml'
    if (lower === 'cargo.lock') return 'toml'
    return ''
  }
  return HL_LANG[ext] || ''
}

// 生成高亮 HTML。lang 为空或语言不可用时直接转义。
export function highlight(code, lang) {
  if (!lang) return esc(code)
  // PHP 混排：代码含 <? 开标签时按 PHP+HTML 混排高亮（VSCode 对 .php 的默认行为）
  if (lang === 'php') lang = /<\?/i.test(code) ? 'php-template' : 'php'
  const hlLang = HL_LANG[lang] || lang
  if (!hljs.getLanguage(hlLang)) return esc(code)
  try {
    // ignoreIllegals: 编辑中的不完整代码不抛错
    return hljs.highlight(code, { language: hlLang, ignoreIllegals: true }).value
  } catch {
    return esc(code)
  }
}

// 保留旧导出（兼容性）
export const CSS_COLORS = {
  keyword: '#c678dd',
  string: '#98c379',
  comment: '#7f848e',
  number: '#d19a66',
  function: '#61afef',
  type: '#e5c07b',
  punctuation: '#abb2bf',
  property: '#e06c75',
  tag: '#e06c75',
  attribute: '#d19a66',
  operator: '#56b6c2'
}
