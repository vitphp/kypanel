<template>
  <div class="nginx-editor" :class="{ fill }" ref="wrapRef">
    <!-- 行号 gutter（VSCode 风格） -->
    <div ref="gutterRef" class="ce-gutter" aria-hidden="true">
      <div
        v-for="n in lineCount"
        :key="n"
        class="ce-gutter-line"
        :class="{ active: n === cursorLine }"
      >{{ n }}</div>
    </div>
    <pre
      ref="highlightRef"
      class="nginx-highlight"
      aria-hidden="true"
      v-html="highlightHtml"
    ></pre>
    <textarea
      ref="editorRef"
      :value="modelValue"
      class="nginx-textarea"
      spellcheck="false"
      autocorrect="off"
      autocapitalize="off"
      :placeholder="placeholder"
      @scroll="onScroll"
      @input="onInput"
      @keydown="onKeydown"
      @keyup="onKeyup"
      @click="onKeyup"
      @blur="onBlur"
    ></textarea>
    <!-- 联想词弹窗（贴光标，含详情面板） -->
    <div
      v-if="completion.visible && completion.items.length > 0"
      class="nginx-complete"
      :style="completion.style"
    >
      <div
        v-for="(it, idx) in completion.items"
        :key="it.label"
        :class="['nginx-complete-item', { active: idx === completion.active }]"
        @mousedown.prevent="accept(it)"
      >
        <span class="nc-kind" :data-kind="it.kind">{{ kindIcon(it.kind) }}</span>
        <span class="nc-label">{{ it.label }}</span>
        <span class="nc-desc">{{ it.desc }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, nextTick, watch } from 'vue'
import { highlight } from '../utils/highlight'

const props = defineProps({
  modelValue: { type: String, default: '' },
  placeholder: { type: String, default: '' },
  // 语言：nginx(默认) | text | json | ini/env | yaml | xml | css | js | shell 等
  lang: { type: String, default: 'nginx' },
  // 禁用联想词
  enableCompletion: { type: Boolean, default: true },
  // 撑满父容器（文件编辑器场景传 true，站点配置场景保持固定高度）
  fill: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue'])

const editorRef = ref()
const highlightRef = ref()
const gutterRef = ref()
const wrapRef = ref()

// 高亮：v-html 直接渲染（nginx 保留手写精确高亮，其余语言走 highlight.js）
const highlightHtml = computed(() => {
  const text = props.modelValue || ''
  return highlightByLang(text, props.lang) + '\n'
})

function highlightByLang(text, lang) {
  if (!text) return ''
  // nginx 无 highlight.js 语言定义，保留手写高亮（SiteSettings 配置文件场景）
  if (lang === 'nginx') return highlightNginx(text)
  // text/plain 等纯文本直接转义
  if (lang === 'text' || lang === 'plain' || lang === '') return escapeHtml(text)
  // 其余语言（js/ts/py/go/php/java/css/html/sql/yaml/json/ini/dockerfile/...）交给 highlight.js
  return highlight(text, lang)
}

function onScroll() {
  if (editorRef.value && highlightRef.value) {
    highlightRef.value.scrollTop = editorRef.value.scrollTop
    highlightRef.value.scrollLeft = editorRef.value.scrollLeft
    if (gutterRef.value) gutterRef.value.scrollTop = editorRef.value.scrollTop
  }
}

function onInput(e) {
  const v = e.target.value
  emit('update:modelValue', v)
  refreshCursorLine()
  updateCompletion()
}

// 行号 & 当前行
const lineCount = computed(() => {
  const t = props.modelValue || ''
  let n = 1
  for (let i = 0; i < t.length; i++) if (t[i] === '\n') n++
  return n
})
const cursorLine = ref(1)
function refreshCursorLine() {
  const ta = editorRef.value
  if (!ta) return
  const pos = ta.selectionStart
  const t = ta.value
  let line = 1
  for (let i = 0; i < pos; i++) if (t[i] === '\n') line++
  cursorLine.value = line
}

// ---------- Nginx 语法高亮 ----------
function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
const NGINX_DIRECTIVES = new Set([
  'http', 'server', 'location', 'upstream', 'events', 'map', 'geo', 'include',
  'listen', 'server_name', 'root', 'index', 'error_page', 'try_files', 'rewrite', 'return',
  'if', 'set', 'break', 'last', 'permanent', 'redirect',
  'proxy_pass', 'proxy_set_header', 'proxy_connect_timeout', 'proxy_read_timeout', 'proxy_send_timeout',
  'fastcgi_pass', 'fastcgi_index', 'fastcgi_param', 'include', 'fastcgi_params',
  'add_header', 'expires', 'access_log', 'error_log', 'limit_rate', 'limit_req', 'limit_conn',
  'client_max_body_size', 'client_body_buffer_size', 'keepalive_timeout', 'gzip', 'gzip_types', 'gzip_min_length',
  'ssl_certificate', 'ssl_certificate_key', 'ssl_protocols', 'ssl_ciphers', 'ssl_session_cache', 'ssl_session_timeout',
  'valid_referers', 'charset', 'default_type', 'types', 'sendfile', 'tcp_nopush', 'tcp_nodelay',
  'worker_processes', 'worker_connections', 'pid', 'user',
  'allow', 'deny'
])
function highlightNginx(text) {
  if (!text) return ''
  return text.split('\n').map(highlightLine).join('\n')
}
function highlightLine(line) {
  if (!line) return ''
  if (/^\s*#/.test(line)) {
    return escapeHtml(line).replace(/^(\s*)(#.*)$/, '$1<span class="hl-comment">$2</span>')
  }
  // 行内注释（引号外）
  let code = line
  let commentTail = ''
  let hashIdx = -1
  let dq = false
  for (let i = 0; i < line.length; i++) {
    const ch = line[i]
    if (ch === '"' && line[i - 1] !== '\\') dq = !dq
    else if (ch === '#' && !dq) { hashIdx = i; break }
  }
  if (hashIdx >= 0) {
    code = line.slice(0, hashIdx)
    commentTail = line.slice(hashIdx)
  }
  const tokenRe = /("(?:\\.|[^"\\])*")|(\$\{[^}]+\}|\$[A-Za-z_][A-Za-z0-9_]*)|([~^]=?)|(\b\d{1,3}(?:\.\d{1,3}){3}\b)|(\b\d+\b)|(\/[^\s;{}]+)|([A-Za-z_][A-Za-z0-9_-]*)|([{};])|(\s+)|(.)/g
  let result = ''
  let m
  let lastIndex = 0
  while ((m = tokenRe.exec(code)) !== null) {
    if (m.index > lastIndex) result += escapeHtml(code.slice(lastIndex, m.index))
    lastIndex = tokenRe.lastIndex
    if (m[1]) result += `<span class="hl-string">${escapeHtml(m[1])}</span>`
    else if (m[2]) result += `<span class="hl-var">${escapeHtml(m[2])}</span>`
    else if (m[3]) result += `<span class="hl-regex">${escapeHtml(m[3])}</span>`
    else if (m[4]) result += `<span class="hl-ip">${escapeHtml(m[4])}</span>`
    else if (m[5]) result += `<span class="hl-number">${escapeHtml(m[5])}</span>`
    else if (m[6]) result += `<span class="hl-path">${escapeHtml(m[6])}</span>`
    else if (m[7]) {
      const word = m[7]
      if (NGINX_DIRECTIVES.has(word)) {
        result += `<span class="hl-keyword">${escapeHtml(word)}</span>`
      } else {
        result += escapeHtml(word)
      }
    }
    else if (m[8]) result += `<span class="hl-brace">${escapeHtml(m[8])}</span>`
    else if (m[9]) result += escapeHtml(m[9])
    else if (m[10]) result += escapeHtml(m[10])
  }
  if (lastIndex < code.length) result += escapeHtml(code.slice(lastIndex))
  if (commentTail) result += `<span class="hl-comment">${escapeHtml(commentTail)}</span>`
  return result
}

// ---------- 联想词（按语言分派） ----------
const NGINX_COMPLETIONS = [
  { label: 'server', kind: 'block', desc: 'server 块（自动展开）', snip: true, _text: 'server {\n    listen       $1;\n    server_name  $2;\n    root         $3;\n    index        index.html index.php;\n\n    location / {\n        try_files $uri $uri/ /index.php?$query_string;\n    }\n\n    $0\n}' },
  { label: 'location', kind: 'block', desc: 'location 块（自动展开）', snip: true, _text: 'location $1 {\n    $0\n}' },
  { label: 'location ~', kind: 'snippet', desc: 'location 正则', snip: true, _text: 'location ~ $1 {\n    $0\n}' },
  { label: 'upstream', kind: 'block', desc: 'upstream 块', snip: true, _text: 'upstream $1 {\n    server 127.0.0.1:$2;\n    $0\n}' },
  { label: 'proxy_pass', kind: 'snippet', desc: '反向代理', snip: true, _text: 'proxy_pass http://$1:$2;' },
  { label: 'ssl_server', kind: 'snippet', desc: 'HTTPS server 块', snip: true, _text: 'server {\n    listen 443 ssl http2;\n    server_name $1;\n    ssl_certificate     /etc/nginx/certs/$2.crt;\n    ssl_certificate_key /etc/nginx/certs/$2.key;\n    ssl_protocols       TLSv1.2 TLSv1.3;\n\n    $0\n}' },
  { label: '301', kind: 'value', desc: '永久重定向' },
  { label: '302', kind: 'value', desc: '临时重定向' },
  { label: '307', kind: 'value', desc: '临时保持方法' },
  { label: '308', kind: 'value', desc: '永久保持方法' },
  { label: '404', kind: 'value', desc: 'Not Found' },
  { label: '500', kind: 'value', desc: 'Server Error' },
  { label: 'on', kind: 'value', desc: '开启' },
  { label: 'off', kind: 'value', desc: '关闭' },
  { label: '=', kind: 'mod', desc: '精确匹配' },
  { label: '~', kind: 'mod', desc: '正则（区分大小写）' },
  { label: '~*', kind: 'mod', desc: '正则（不区分大小写）' },
  { label: '^~', kind: 'mod', desc: '前缀优先匹配' },
  { label: '$host', kind: 'var', desc: '请求主机名' },
  { label: '$request_uri', kind: 'var', desc: '完整 URL 含 query' },
  { label: '$uri', kind: 'var', desc: '当前 URL 路径' },
  { label: '$args', kind: 'var', desc: 'query string' },
  { label: '$remote_addr', kind: 'var', desc: '客户端 IP' },
  { label: '$document_root', kind: 'var', desc: '当前 root' },
  { label: '$proxy_add_x_forwarded_for', kind: 'var', desc: 'X-Forwarded-For' },
  { label: '$scheme', kind: 'var', desc: 'http/https' },
  { label: 'listen 80', kind: 'value', desc: '监听 80' },
  { label: 'listen 443', kind: 'value', desc: '监听 443' },
  { label: 'listen 8080', kind: 'value', desc: '监听 8080' }
]

// ---- 各语言关键字/片段补全（label=插入文本, kind=类别, desc=说明, snip=是否片段）----
// 片段语法：$0=光标落点；tab 跳转点按 $1/$2... 顺序。仅简单片段，避免复杂。
const JS_COMPLETIONS = [
  { label: 'function', kind: 'keyword', desc: '函数声明' },
  { label: 'function $1($2) {\n  $0\n}', kind: 'snippet', desc: '函数声明（含参数）', snip: true },
  { label: 'const', kind: 'keyword', desc: '常量声明' },
  { label: 'let', kind: 'keyword', desc: '变量声明' },
  { label: 'var', kind: 'keyword', desc: '变量声明' },
  { label: 'return', kind: 'keyword', desc: '返回值' },
  { label: 'if', kind: 'keyword', desc: '条件' },
  { label: 'if ($1) {\n  $0\n}', kind: 'snippet', desc: 'if 块', snip: true },
  { label: 'if else', kind: 'snippet', desc: 'if/else 块', snip: true },
  { label: 'else', kind: 'keyword', desc: '否则' },
  { label: 'for', kind: 'keyword', desc: 'for 循环' },
  { label: 'for (let $1 = 0; $1 < $2; $1++) {\n  $0\n}', kind: 'snippet', desc: 'for 计数循环', snip: true },
  { label: 'forin', kind: 'snippet', desc: 'for...in 遍历', snip: true },
  { label: 'forof', kind: 'snippet', desc: 'for...of 遍历', snip: true },
  { label: 'while', kind: 'keyword', desc: 'while 循环' },
  { label: 'while ($1) {\n  $0\n}', kind: 'snippet', desc: 'while 块', snip: true },
  { label: 'switch', kind: 'keyword', desc: '分支' },
  { label: 'switch ($1) {\n  case $2:\n    $0\n    break\n  default:\n    break\n}', kind: 'snippet', desc: 'switch 分支', snip: true },
  { label: 'case', kind: 'keyword', desc: '分支项' },
  { label: 'break', kind: 'keyword', desc: '跳出循环' },
  { label: 'continue', kind: 'keyword', desc: '继续循环' },
  { label: 'try', kind: 'keyword', desc: '异常捕获' },
  { label: 'try {\n  $1\n} catch (e) {\n  $0\n}', kind: 'snippet', desc: 'try/catch 块', snip: true },
  { label: 'catch', kind: 'keyword', desc: '捕获异常' },
  { label: 'finally', kind: 'keyword', desc: '最终执行' },
  { label: 'throw', kind: 'keyword', desc: '抛出异常' },
  { label: 'new', kind: 'keyword', desc: '实例化' },
  { label: 'class', kind: 'keyword', desc: '类声明' },
  { label: 'class $1 {\n  constructor($2) {\n    $0\n  }\n}', kind: 'snippet', desc: 'class 声明', snip: true },
  { label: 'extends', kind: 'keyword', desc: '继承' },
  { label: 'import', kind: 'keyword', desc: '导入' },
  { label: 'export', kind: 'keyword', desc: '导出' },
  { label: 'async', kind: 'keyword', desc: '异步' },
  { label: 'await', kind: 'keyword', desc: '等待' },
  { label: 'async function', kind: 'snippet', desc: 'async 函数', snip: true },
  { label: '=>', kind: 'operator', desc: '箭头函数' },
  { label: 'console.log($1)', kind: 'snippet', desc: '打印日志', snip: true },
  { label: 'console.error($1)', kind: 'snippet', desc: '打印错误', snip: true },
  { label: 'typeof', kind: 'keyword', desc: '类型判断' },
  { label: 'instanceof', kind: 'keyword', desc: '实例判断' },
  { label: 'true', kind: 'value', desc: '布尔真' },
  { label: 'false', kind: 'value', desc: '布尔假' },
  { label: 'null', kind: 'value', desc: '空值' },
  { label: 'undefined', kind: 'value', desc: '未定义' }
]

const TS_COMPLETIONS = [
  { label: 'interface', kind: 'keyword', desc: '接口' },
  { label: 'type', kind: 'keyword', desc: '类型别名' },
  { label: 'enum', kind: 'keyword', desc: '枚举' },
  { label: 'implements', kind: 'keyword', desc: '实现接口' },
  { label: 'readonly', kind: 'keyword', desc: '只读' },
  { label: 'private', kind: 'keyword', desc: '私有' },
  { label: 'public', kind: 'keyword', desc: '公有' },
  { label: 'protected', kind: 'keyword', desc: '受保护' },
  { label: 'abstract', kind: 'keyword', desc: '抽象' },
  { label: 'string', kind: 'type', desc: '字符串类型' },
  { label: 'number', kind: 'type', desc: '数字类型' },
  { label: 'boolean', kind: 'type', desc: '布尔类型' },
  { label: 'any', kind: 'type', desc: '任意类型' },
  { label: 'void', kind: 'type', desc: '无返回值' },
  { label: 'unknown', kind: 'type', desc: '未知类型' },
  { label: 'Promise', kind: 'type', desc: 'Promise 类型' }
]

const PY_COMPLETIONS = [
  { label: 'def', kind: 'keyword', desc: '函数定义' },
  { label: 'def $1($2):\n    $0', kind: 'snippet', desc: '函数定义（含参数）', snip: true },
  { label: 'class', kind: 'keyword', desc: '类定义' },
  { label: 'class $1:\n    def __init__(self$2):\n        $0', kind: 'snippet', desc: '类定义（含 init）', snip: true },
  { label: 'import', kind: 'keyword', desc: '导入模块' },
  { label: 'from', kind: 'keyword', desc: '导入' },
  { label: 'return', kind: 'keyword', desc: '返回值' },
  { label: 'if', kind: 'keyword', desc: '条件' },
  { label: 'if $1:\n    $0', kind: 'snippet', desc: 'if 块', snip: true },
  { label: 'ifelse', kind: 'snippet', desc: 'if/else 块', snip: true },
  { label: 'elif', kind: 'keyword', desc: '否则条件' },
  { label: 'else', kind: 'keyword', desc: '否则' },
  { label: 'for', kind: 'keyword', desc: 'for 循环' },
  { label: 'for $1 in $2:\n    $0', kind: 'snippet', desc: 'for 遍历', snip: true },
  { label: 'while', kind: 'keyword', desc: 'while 循环' },
  { label: 'while $1:\n    $0', kind: 'snippet', desc: 'while 块', snip: true },
  { label: 'break', kind: 'keyword', desc: '跳出循环' },
  { label: 'continue', kind: 'keyword', desc: '继续循环' },
  { label: 'try', kind: 'keyword', desc: '异常捕获' },
  { label: 'try:\n    $1\nexcept Exception as e:\n    $0', kind: 'snippet', desc: 'try/except 块', snip: true },
  { label: 'except', kind: 'keyword', desc: '捕获异常' },
  { label: 'finally', kind: 'keyword', desc: '最终执行' },
  { label: 'raise', kind: 'keyword', desc: '抛出异常' },
  { label: 'with', kind: 'keyword', desc: '上下文管理' },
  { label: 'with $1 as $2:\n    $0', kind: 'snippet', desc: 'with 块', snip: true },
  { label: 'lambda', kind: 'keyword', desc: '匿名函数' },
  { label: 'pass', kind: 'keyword', desc: '占位' },
  { label: 'print($1)', kind: 'snippet', desc: '打印', snip: true },
  { label: 'if __name__ == "__main__":\n    $0', kind: 'snippet', desc: '主入口', snip: true },
  { label: 'self', kind: 'keyword', desc: '实例自身' },
  { label: 'None', kind: 'value', desc: '空值' },
  { label: 'True', kind: 'value', desc: '布尔真' },
  { label: 'False', kind: 'value', desc: '布尔假' },
  { label: 'async def', kind: 'keyword', desc: '异步函数' },
  { label: 'await', kind: 'keyword', desc: '等待' }
]

const GO_COMPLETIONS = [
  { label: 'func', kind: 'keyword', desc: '函数声明' },
  { label: 'func $1($2) $3 {\n  $0\n}', kind: 'snippet', desc: '函数声明', snip: true },
  { label: 'package', kind: 'keyword', desc: '包声明' },
  { label: 'import', kind: 'keyword', desc: '导入包' },
  { label: 'var', kind: 'keyword', desc: '变量声明' },
  { label: 'const', kind: 'keyword', desc: '常量声明' },
  { label: 'type', kind: 'keyword', desc: '类型声明' },
  { label: 'type $1 struct {\n  $0\n}', kind: 'snippet', desc: 'struct 类型', snip: true },
  { label: 'type $1 interface {\n  $0\n}', kind: 'snippet', desc: 'interface 类型', snip: true },
  { label: 'struct', kind: 'keyword', desc: '结构体' },
  { label: 'interface', kind: 'keyword', desc: '接口' },
  { label: 'return', kind: 'keyword', desc: '返回值' },
  { label: 'if', kind: 'keyword', desc: '条件' },
  { label: 'if $1 {\n  $0\n}', kind: 'snippet', desc: 'if 块', snip: true },
  { label: 'if err != nil', kind: 'snippet', desc: '错误处理', snip: true },
  { label: 'else', kind: 'keyword', desc: '否则' },
  { label: 'for', kind: 'keyword', desc: 'for 循环' },
  { label: 'for $1 {\n  $0\n}', kind: 'snippet', desc: 'for 无限循环', snip: true },
  { label: 'fori', kind: 'snippet', desc: 'for 计数循环', snip: true },
  { label: 'forr', kind: 'snippet', desc: 'for range 遍历', snip: true },
  { label: 'range', kind: 'keyword', desc: '遍历' },
  { label: 'switch', kind: 'keyword', desc: '分支' },
  { label: 'switch $1 {\ncase $2:\n  $0\n}', kind: 'snippet', desc: 'switch 块', snip: true },
  { label: 'case', kind: 'keyword', desc: '分支项' },
  { label: 'break', kind: 'keyword', desc: '跳出' },
  { label: 'continue', kind: 'keyword', desc: '继续' },
  { label: 'defer', kind: 'keyword', desc: '延迟执行' },
  { label: 'go', kind: 'keyword', desc: '并发' },
  { label: 'chan', kind: 'keyword', desc: '通道' },
  { label: 'map', kind: 'keyword', desc: '映射' },
  { label: 'make', kind: 'keyword', desc: '初始化' },
  { label: 'new', kind: 'keyword', desc: '分配内存' },
  { label: 'fmt.Println($1)', kind: 'snippet', desc: '打印', snip: true },
  { label: 'error', kind: 'type', desc: '错误类型' },
  { label: 'string', kind: 'type', desc: '字符串' },
  { label: 'int', kind: 'type', desc: '整数' },
  { label: 'bool', kind: 'type', desc: '布尔' },
  { label: 'nil', kind: 'value', desc: '空值' },
  { label: 'true', kind: 'value', desc: '布尔真' },
  { label: 'false', kind: 'value', desc: '布尔假' }
]

const PHP_COMPLETIONS = [
  { label: 'function', kind: 'keyword', desc: '函数声明' },
  { label: 'function $1($2) {\n  $0\n}', kind: 'snippet', desc: '函数声明', snip: true },
  { label: 'class', kind: 'keyword', desc: '类声明' },
  { label: 'class $1 {\n  public function __construct($2) {\n    $0\n  }\n}', kind: 'snippet', desc: '类声明', snip: true },
  { label: 'echo', kind: 'keyword', desc: '输出' },
  { label: 'echo $1;', kind: 'snippet', desc: '输出语句', snip: true },
  { label: 'return', kind: 'keyword', desc: '返回值' },
  { label: 'if', kind: 'keyword', desc: '条件' },
  { label: 'if ($1) {\n  $0\n}', kind: 'snippet', desc: 'if 块', snip: true },
  { label: 'else', kind: 'keyword', desc: '否则' },
  { label: 'elseif', kind: 'keyword', desc: '否则条件' },
  { label: 'foreach', kind: 'keyword', desc: '遍历' },
  { label: 'foreach ($1 as $2) {\n  $0\n}', kind: 'snippet', desc: 'foreach 遍历', snip: true },
  { label: 'for', kind: 'keyword', desc: 'for 循环' },
  { label: 'for ($1; $2; $3) {\n  $0\n}', kind: 'snippet', desc: 'for 循环', snip: true },
  { label: 'while', kind: 'keyword', desc: 'while 循环' },
  { label: 'switch', kind: 'keyword', desc: '分支' },
  { label: 'try', kind: 'keyword', desc: '异常' },
  { label: 'try {\n  $1\n} catch (\\Exception $e) {\n  $0\n}', kind: 'snippet', desc: 'try/catch 块', snip: true },
  { label: 'catch', kind: 'keyword', desc: '捕获' },
  { label: 'public', kind: 'keyword', desc: '公有' },
  { label: 'private', kind: 'keyword', desc: '私有' },
  { label: 'protected', kind: 'keyword', desc: '受保护' },
  { label: 'static', kind: 'keyword', desc: '静态' },
  { label: 'namespace', kind: 'keyword', desc: '命名空间' },
  { label: 'use', kind: 'keyword', desc: '导入' },
  { label: '<?php', kind: 'snippet', desc: 'PHP 开始标签', snip: true },
  { label: '<?php\n$0', kind: 'snippet', desc: 'PHP 块', snip: true },
  { label: 'new', kind: 'keyword', desc: '实例化' },
  { label: '$this', kind: 'keyword', desc: '当前对象' },
  { label: 'var_dump($1)', kind: 'snippet', desc: '调试输出', snip: true },
  { label: 'print_r($1)', kind: 'snippet', desc: '打印数组', snip: true }
]

const JAVA_COMPLETIONS = [
  { label: 'class', kind: 'keyword', desc: '类声明' },
  { label: 'public', kind: 'keyword', desc: '公有' },
  { label: 'private', kind: 'keyword', desc: '私有' },
  { label: 'protected', kind: 'keyword', desc: '受保护' },
  { label: 'static', kind: 'keyword', desc: '静态' },
  { label: 'final', kind: 'keyword', desc: '不可变' },
  { label: 'void', kind: 'keyword', desc: '无返回' },
  { label: 'int', kind: 'type', desc: '整数' },
  { label: 'String', kind: 'type', desc: '字符串' },
  { label: 'boolean', kind: 'type', desc: '布尔' },
  { label: 'return', kind: 'keyword', desc: '返回值' },
  { label: 'if', kind: 'keyword', desc: '条件' },
  { label: 'else', kind: 'keyword', desc: '否则' },
  { label: 'for', kind: 'keyword', desc: 'for 循环' },
  { label: 'while', kind: 'keyword', desc: 'while 循环' },
  { label: 'switch', kind: 'keyword', desc: '分支' },
  { label: 'try', kind: 'keyword', desc: '异常' },
  { label: 'catch', kind: 'keyword', desc: '捕获' },
  { label: 'throw', kind: 'keyword', desc: '抛出' },
  { label: 'new', kind: 'keyword', desc: '实例化' },
  { label: 'this', kind: 'keyword', desc: '当前对象' }
]

const C_COMPLETIONS = [
  { label: '#include', kind: 'keyword', desc: '包含头文件' },
  { label: '#define', kind: 'keyword', desc: '宏定义' },
  { label: 'int', kind: 'type', desc: '整数' },
  { label: 'char', kind: 'type', desc: '字符' },
  { label: 'float', kind: 'type', desc: '浮点' },
  { label: 'double', kind: 'type', desc: '双精度' },
  { label: 'void', kind: 'type', desc: '无返回' },
  { label: 'struct', kind: 'keyword', desc: '结构体' },
  { label: 'typedef', kind: 'keyword', desc: '类型别名' },
  { label: 'return', kind: 'keyword', desc: '返回值' },
  { label: 'if', kind: 'keyword', desc: '条件' },
  { label: 'else', kind: 'keyword', desc: '否则' },
  { label: 'for', kind: 'keyword', desc: 'for 循环' },
  { label: 'while', kind: 'keyword', desc: 'while 循环' },
  { label: 'switch', kind: 'keyword', desc: '分支' },
  { label: 'break', kind: 'keyword', desc: '跳出' },
  { label: 'continue', kind: 'keyword', desc: '继续' },
  { label: 'sizeof', kind: 'keyword', desc: '大小' }
]

const CSS_COMPLETIONS = [
  { label: 'display', kind: 'property', desc: '显示方式' },
  { label: 'position', kind: 'property', desc: '定位' },
  { label: 'margin', kind: 'property', desc: '外边距' },
  { label: 'padding', kind: 'property', desc: '内边距' },
  { label: 'width', kind: 'property', desc: '宽度' },
  { label: 'height', kind: 'property', desc: '高度' },
  { label: 'color', kind: 'property', desc: '文字颜色' },
  { label: 'background', kind: 'property', desc: '背景' },
  { label: 'background-color', kind: 'property', desc: '背景色' },
  { label: 'border', kind: 'property', desc: '边框' },
  { label: 'border-radius', kind: 'property', desc: '圆角' },
  { label: 'font-size', kind: 'property', desc: '字号' },
  { label: 'font-weight', kind: 'property', desc: '字重' },
  { label: 'text-align', kind: 'property', desc: '对齐' },
  { label: 'flex', kind: 'property', desc: '弹性布局' },
  { label: 'overflow', kind: 'property', desc: '溢出' },
  { label: 'z-index', kind: 'property', desc: '层级' },
  { label: 'transition', kind: 'property', desc: '过渡' },
  { label: 'transform', kind: 'property', desc: '变换' },
  { label: 'opacity', kind: 'property', desc: '透明度' },
  { label: 'cursor', kind: 'property', desc: '光标' }
]

const HTML_COMPLETIONS = [
  { label: 'div', kind: 'tag', desc: '块级容器' },
  { label: 'divb', kind: 'tag', desc: 'div 容器（自动展开）', snip: true, _text: '<' + 'div' + '>$0</' + 'div' + '>' },
  { label: 'span', kind: 'tag', desc: '行内容器' },
  { label: 'spanb', kind: 'tag', desc: 'span 容器（自动展开）', snip: true, _text: '<' + 'span' + '>$0</' + 'span' + '>' },
  { label: 'p', kind: 'tag', desc: '段落' },
  { label: 'pb', kind: 'tag', desc: 'p 段落（自动展开）', snip: true, _text: '<' + 'p' + '>$0</' + 'p' + '>' },
  { label: 'a', kind: 'tag', desc: '链接' },
  { label: 'ahref', kind: 'tag', desc: 'a 链接（自动展开）', snip: true, _text: '<' + 'a href="$1"' + '>$0</' + 'a' + '>' },
  { label: 'img', kind: 'tag', desc: '图片' },
  { label: 'imgsrc', kind: 'tag', desc: 'img 标签（自动展开）', snip: true, _text: '<' + 'img src="$1" alt="$2" /' + '>' },
  { label: 'ul', kind: 'tag', desc: '无序列表' },
  { label: 'ulb', kind: 'tag', desc: 'ul 列表（自动展开）', snip: true, _text: '<' + 'ul' + '>\n  ' + '<' + 'li' + '>$0</' + 'li' + '>\n</' + 'ul' + '>' },
  { label: 'li', kind: 'tag', desc: '列表项' },
  { label: 'table', kind: 'tag', desc: '表格' },
  { label: 'form', kind: 'tag', desc: '表单' },
  { label: 'input', kind: 'tag', desc: '输入框' },
  { label: 'button', kind: 'tag', desc: '按钮' },
  { label: 'h1', kind: 'tag', desc: '标题1' },
  { label: 'h2', kind: 'tag', desc: '标题2' },
  { label: 'h3', kind: 'tag', desc: '标题3' },
  { label: 'br', kind: 'tag', desc: '换行' },
  { label: 'hr', kind: 'tag', desc: '分隔线' },
  { label: 'script', kind: 'tag', desc: '脚本' },
  { label: 'scriptb', kind: 'tag', desc: 'script 块（自动展开）', snip: true, _text: '<' + 'script' + '>$0</' + 'script' + '>' },
  { label: 'style', kind: 'tag', desc: '样式' },
  { label: 'link', kind: 'tag', desc: '资源链接' },
  { label: 'meta', kind: 'tag', desc: '元信息' },
  { label: 'html5', kind: 'snippet', desc: 'HTML5 文档骨架', snip: true,
    _text: '<!DOCTYPE html>\n' + '<' + 'html lang="zh-CN"' + '>\n' + '<' + 'head' + '>\n  ' + '<' + 'meta charset="UTF-8"' + '>\n  ' + '<' + 'title' + '>$1</' + 'title' + '>\n</' + 'head' + '>\n' + '<' + 'body' + '>\n  $0\n</' + 'body' + '>\n</' + 'html' + '>' },
  { label: 'head', kind: 'tag', desc: '文档头' },
  { label: 'body', kind: 'tag', desc: '文档体' }
]

const SH_COMPLETIONS = [
  { label: 'if', kind: 'keyword', desc: '条件' },
  { label: 'then', kind: 'keyword', desc: '条件体' },
  { label: 'else', kind: 'keyword', desc: '否则' },
  { label: 'fi', kind: 'keyword', desc: '结束条件' },
  { label: 'for', kind: 'keyword', desc: 'for 循环' },
  { label: 'while', kind: 'keyword', desc: 'while 循环' },
  { label: 'do', kind: 'keyword', desc: '循环体' },
  { label: 'done', kind: 'keyword', desc: '结束循环' },
  { label: 'case', kind: 'keyword', desc: '分支' },
  { label: 'esac', kind: 'keyword', desc: '结束分支' },
  { label: 'echo', kind: 'keyword', desc: '输出' },
  { label: 'exit', kind: 'keyword', desc: '退出' },
  { label: 'export', kind: 'keyword', desc: '导出变量' },
  { label: 'local', kind: 'keyword', desc: '局部变量' },
  { label: 'function', kind: 'keyword', desc: '函数' },
  { label: '#!/bin/bash', kind: 'snippet', desc: 'Shebang', snip: true },
  { label: '$1', kind: 'var', desc: '位置参数' },
  { label: '$?', kind: 'var', desc: '退出码' }
]

const SQL_COMPLETIONS = [
  { label: 'SELECT', kind: 'keyword', desc: '查询' },
  { label: 'FROM', kind: 'keyword', desc: '来源表' },
  { label: 'WHERE', kind: 'keyword', desc: '条件' },
  { label: 'INSERT', kind: 'keyword', desc: '插入' },
  { label: 'INTO', kind: 'keyword', desc: '目标表' },
  { label: 'UPDATE', kind: 'keyword', desc: '更新' },
  { label: 'DELETE', kind: 'keyword', desc: '删除' },
  { label: 'CREATE', kind: 'keyword', desc: '创建' },
  { label: 'TABLE', kind: 'keyword', desc: '表' },
  { label: 'DROP', kind: 'keyword', desc: '删除对象' },
  { label: 'ALTER', kind: 'keyword', desc: '修改' },
  { label: 'JOIN', kind: 'keyword', desc: '连接' },
  { label: 'LEFT', kind: 'keyword', desc: '左连接' },
  { label: 'ORDER', kind: 'keyword', desc: '排序' },
  { label: 'BY', kind: 'keyword', desc: '依据' },
  { label: 'GROUP', kind: 'keyword', desc: '分组' },
  { label: 'LIMIT', kind: 'keyword', desc: '限制条数' },
  { label: 'COUNT', kind: 'keyword', desc: '计数' },
  { label: 'DISTINCT', kind: 'keyword', desc: '去重' },
  { label: 'NOT', kind: 'keyword', desc: '非' },
  { label: 'NULL', kind: 'keyword', desc: '空值' }
]

// 语言 → 补全表（nginx 走 NGINX_COMPLETIONS，其余按 lang 关键字分派）
function getCompletions(lang) {
  const l = (lang || '').toLowerCase()
  switch (l) {
    case 'nginx': return NGINX_COMPLETIONS
    case 'js': case 'mjs': case 'cjs': case 'jsx':
    case 'javascript': return JS_COMPLETIONS
    case 'ts': case 'tsx': case 'typescript': return TS_COMPLETIONS
    case 'py': case 'python': return PY_COMPLETIONS
    case 'go': return GO_COMPLETIONS
    case 'php': return PHP_COMPLETIONS
    case 'java': return JAVA_COMPLETIONS
    case 'c': case 'h': case 'cpp': case 'cc': case 'hpp': case 'cs': return C_COMPLETIONS
    case 'css': case 'scss': case 'less': return CSS_COMPLETIONS
    case 'html': case 'htm': case 'xml': case 'svg': case 'vue': return HTML_COMPLETIONS
    case 'sh': case 'bash': case 'shell': case 'zsh': return SH_COMPLETIONS
    case 'sql': return SQL_COMPLETIONS
    default: return []
  }
}

// 记录上一次匹配的前缀 + 起始偏移；仅当 prefix / startOffset 变化时重置 active = 0，
// 避免 onKeyup 重复刷新把高亮打回第一项
let lastPrefix = ''
let lastStart = -1

const completion = reactive({
  visible: false,
  items: [],
  active: 0,
  startOffset: 0,
  // 弹窗定位：相对 CodeEditor 容器（绝对定位）
  style: { top: '0px', left: '0px' }
})

// 取当前光标前的"单词前缀"。字符类放宽：字母数字下划线 + 常见符号（@ $ . # -）。
function getCurrentWordPrefix() {
  if (!editorRef.value) return null
  const ta = editorRef.value
  const pos = ta.selectionStart
  const text = ta.value
  let i = pos - 1
  while (i >= 0 && /[A-Za-z0-9_\-@$.#]/.test(text[i])) i--
  const start = i + 1
  const prefix = text.slice(start, pos)
  if (!prefix) return null
  return { prefix, start, end: pos }
}

// 把光标绝对像素位置转成相对 wrapRef 容器的坐标，用于联想弹窗贴光标
function caretPosInContainer() {
  const ta = editorRef.value
  const wrap = wrapRef.value
  if (!ta || !wrap) return { left: 0, top: 0, lineHeight: 20 }
  // 复制 textarea 计算样式
  const taStyle = window.getComputedStyle(ta)
  const wrapRect = wrap.getBoundingClientRect()
  // 用 mirror div 测量光标前文本宽，得到相对 textarea 左上角的偏移
  const mirror = document.createElement('div')
  mirror.style.cssText = `
    position: absolute; visibility: hidden; white-space: pre; pointer-events: none;
    font: ${taStyle.font}; line-height: ${taStyle.lineHeight};
    padding: ${taStyle.paddingTop} ${taStyle.paddingRight} ${taStyle.paddingBottom} ${taStyle.paddingLeft};
    border: 0; box-sizing: border-box;`
  document.body.appendChild(mirror)
  const pos = ta.selectionStart
  const textBefore = ta.value.substring(0, pos)
  // 行高：line-height
  const lineHeight = parseFloat(taStyle.lineHeight) || 20
  // 拆分最后一行
  const lastNL = textBefore.lastIndexOf('\n')
  const lastLine = textBefore.substring(lastNL + 1)
  mirror.textContent = lastLine
  const x = mirror.scrollWidth
  const y = (pos === 0) ? 0 : (textBefore.split('\n').length - 1) * lineHeight
  document.body.removeChild(mirror)
  // textarea 的 getBoundingClientRect 包含 wrap 内偏移
  const taRect = ta.getBoundingClientRect()
  return {
    left: (taRect.left - wrapRect.left) + x - ta.scrollLeft,
    top: (taRect.top - wrapRect.top) + y - ta.scrollTop,
    lineHeight
  }
}

function updateCompletion() {
  if (!props.enableCompletion) {
    completion.visible = false
    lastPrefix = ''
    return
  }
  const info = getCurrentWordPrefix()
  if (!info || info.prefix.length < 1) {
    completion.visible = false
    lastPrefix = ''
    return
  }
  const p = info.prefix.toLowerCase()
  // 只有当前缀 / 起始偏移变化时才重置 active=0（避免 ↑↓ 导航被 keyup 刷新回第一项）
  const prefixChanged = p !== lastPrefix || info.start !== lastStart
  const matches = getCompletions(props.lang)
    .filter((it) => it.label.toLowerCase().startsWith(p))
    .slice(0, 10)
  if (matches.length === 0) {
    completion.visible = false
    lastPrefix = ''
    return
  }
  completion.items = matches
  if (prefixChanged) {
    // 保持当前 active 在合理范围内（用户已通过方向键选过的索引）
    const prev = completion.active
    completion.active = prev < matches.length ? prev : 0
    completion.startOffset = info.start
  }
  lastPrefix = p
  lastStart = info.start
  // 贴光标下方一行（VSCode 风格）；空间不足时弹上方
  const cp = caretPosInContainer()
  const wrap = wrapRef.value
  const wrapH = wrap ? wrap.clientHeight : 400
  const popH = 28 + matches.length * 28 // 估算
  let top = cp.top + cp.lineHeight + 2
  if (top + popH > wrapH) top = Math.max(0, cp.top - popH - 2)
  completion.style = {
    left: Math.min(Math.max(0, cp.left), (wrap?.clientWidth || 600) - 320) + 'px',
    top: top + 'px'
  }
  completion.visible = true
}

// 片段：$0 = 最终光标；$1/$2/... = 跳转点（依次 Tab 切换，跳到最后一个后到 $0）。
// 多个相同占位符同步修改。
function applySnippet(text) {
  const stops = []
  const re = /\$(\d+)|\$0/g
  let m
  while ((m = re.exec(text)) !== null) {
    const idx = m[0] === '$0' ? 0 : parseInt(m[1], 10)
    stops.push({ index: m.index, len: m[0].length, idx })
  }
  if (stops.length === 0) {
    return { text, cursor: text.length, stops: [] }
  }
  // 去占位符，保留位点位置
  const stopsOut = []
  let out = ''
  let last = 0
  for (const s of stops) {
    out += text.substring(last, s.index)
    stopsOut.push({ pos: out.length, idx: s.idx })
    last = s.index + s.len
  }
  out += text.substring(last)
  return { text: out, cursor: stopsOut[0].pos, stops: stopsOut }
}

// 接收联想项：插入文本 + 跳转光标 + 暴露跳转点（onMounted 时挂到全局 stopStack）
const pendingStops = ref([])
const stopStack = []
function accept(item) {
  if (!editorRef.value) return
  const ta = editorRef.value
  const start = completion.startOffset
  const pos = ta.selectionStart
  // _text 优先：允许 label 用作联想关键字（用户敲的），_text 是实际插入的片段
  const text = item._text || item.label
  const snip = applySnippet(text)
  const before = props.modelValue.slice(0, start)
  const after = props.modelValue.slice(pos)
  const newVal = before + snip.text + after
  emit('update:modelValue', newVal)
  // 计算新光标位置（考虑插入文本 + after 偏移）
  const cursorInInsert = snip.cursor
  const newPos = start + cursorInInsert
  pendingStops.value = snip.stops.map(s => ({ pos: start + s.pos, idx: s.idx }))
  nextTick(() => {
    ta.focus()
    ta.setSelectionRange(newPos, newPos)
    completion.visible = false
    refreshCursorLine()
  })
}

function onKeydown(e) {
  // 联想弹窗开启时拦截方向键 / Enter / Tab / Esc
  if (completion.visible) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      completion.active = (completion.active + 1) % completion.items.length
      return
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      completion.active = (completion.active - 1 + completion.items.length) % completion.items.length
      return
    } else if (e.key === 'Enter' || e.key === 'Tab') {
      if (completion.items.length) {
        e.preventDefault()
        accept(completion.items[completion.active])
      }
      return
    } else if (e.key === 'Escape') {
      e.preventDefault()
      completion.visible = false
      return
    }
  }
  // 片段跳转：Tab 切换到下一个 stop（同 idx 的会一起选中）
  if (e.key === 'Tab' && stopStack.length > 0) {
    e.preventDefault()
    const ta = editorRef.value
    if (!ta) return
    const top = stopStack[0]
    const rest = stopStack.filter(s => s.idx === top.idx)
    // 选中当前 idx 的所有 stop
    const selStart = rest[0].pos
    const selEnd = rest[rest.length - 1].pos
    ta.setSelectionRange(selStart, selEnd)
    stopStack.shift()
    if (stopStack.length === 0) {
      // 到末尾 → 取消选中，光标落在 stop 位置
      nextTick(() => ta.setSelectionRange(selEnd, selEnd))
    }
    return
  }
  // 普通 Tab：缩进 / 反缩进（拦截浏览器默认焦点跳转）
  if (e.key === 'Tab') {
    e.preventDefault()
    e.stopPropagation()
    handleIndent(e.shiftKey)
    return
  }
  // 自动配对：() [] {} "" '' <> （仅纯字符输入，不影响有选区的情况由下方覆盖）
  handleAutoPair(e)
  // HTML/XML 标签自动闭合：输入 > 时补 </tag>
  handleTagClose(e)
}

// Tab 缩进 / Shift+Tab 反缩进（2 空格）
function handleIndent(back) {
  const ta = editorRef.value
  if (!ta) return
  const start = ta.selectionStart
  const end = ta.selectionEnd
  const text = ta.value
  // 无选区：单行缩进
  if (start === end) {
    if (back) {
      // 反缩进：删除光标前的空白（最多 2 空格）
      const lineStart = text.lastIndexOf('\n', start - 1) + 1
      const leading = text.substring(lineStart, start)
      const ws = leading.match(/[ \t]+$/)
      if (!ws) return
      const remove = Math.min(ws[0].length, 2)
      const newPos = start - remove
      emit('update:modelValue', text.substring(0, newPos) + text.substring(start))
      nextTick(() => {
        const el = editorRef.value
        if (el) {
          el.focus({ preventScroll: true })
          el.setSelectionRange(newPos, newPos)
        }
        refreshCursorLine()
      })
    } else {
      const newVal = text.substring(0, start) + '  ' + text.substring(start)
      emit('update:modelValue', newVal)
      nextTick(() => {
        const el = editorRef.value
        if (el) {
          el.focus({ preventScroll: true })
          el.setSelectionRange(start + 2, start + 2)
        }
        refreshCursorLine()
      })
    }
    return
  }
  // 多行选区：整块缩进/反缩进
  const lineStart = text.lastIndexOf('\n', start - 1) + 1
  const lineEnd = text.indexOf('\n', end)
  const blockEnd = lineEnd === -1 ? text.length : lineEnd
  const block = text.substring(lineStart, blockEnd)
  const lines = block.split('\n')
  if (back) {
    const newLines = lines.map(l => l.replace(/^ {1,2}/, ''))
    const newBlock = newLines.join('\n')
    const removed = block.length - newBlock.length
    emit('update:modelValue', text.substring(0, lineStart) + newBlock + text.substring(blockEnd))
    nextTick(() => {
      ta.focus()
      ta.setSelectionRange(start, Math.max(start, end - removed))
      refreshCursorLine()
    })
  } else {
    const newLines = lines.map(l => '  ' + l)
    const newBlock = newLines.join('\n')
    emit('update:modelValue', text.substring(0, lineStart) + newBlock + text.substring(blockEnd))
    nextTick(() => {
      ta.focus()
      ta.setSelectionRange(start + 2, end + newLines.length * 2)
      refreshCursorLine()
    })
  }
}

// 标记类语言（标签自动闭合适用）
const MARKUP_LANGS = new Set(['html', 'htm', 'xml', 'svg', 'vue', 'jsx', 'tsx'])
// 无需闭合的自闭合/空标签
const VOID_TAGS = new Set(['img', 'br', 'hr', 'input', 'meta', 'link', 'area', 'base', 'col', 'embed', 'source', 'track', 'wbr'])

function handleTagClose(e) {
  if (e.type !== 'keydown' || e.key !== '>') return
  if (e.ctrlKey || e.metaKey || e.altKey || e.isComposing) return
  const lang = (props.lang || '').toLowerCase()
  if (!MARKUP_LANGS.has(lang)) return
  const ta = editorRef.value
  if (!ta) return
  const pos = ta.selectionStart
  const text = ta.value
  // 找光标前最近的未闭合 "<"（从光标往前扫，忽略已闭合的）
  const before = text.substring(0, pos)
  const ltIdx = before.lastIndexOf('<')
  if (ltIdx === -1) return
  const afterLt = before.substring(ltIdx + 1)
  // 若 "<" 之后已含 ">" 说明这个 < 已闭合（如 <div>），不处理；只处理正在输入的开标签 "<div"（无 >）
  if (afterLt.includes('>')) return
  // 提取标签名
  const m = afterLt.match(/^([a-zA-Z][a-zA-Z0-9-]*)/)
  if (!m) return
  const tag = m[1].toLowerCase()
  if (VOID_TAGS.has(tag)) return // 自闭合标签不补
  // 已存在对应闭合标签则跳过（避免重复补）
  const rest = text.substring(pos)
  const closeRe = new RegExp('^\\s*</' + tag + '>')
  if (closeRe.test(rest)) return
  // 在光标处插入 </tag>
  e.preventDefault()
  const newVal = before + '></' + tag + '>' + rest
  emit('update:modelValue', newVal)
  // 光标停在标签内部：<tag>|</tag>
  const caret = pos + 1
  nextTick(() => {
    ta.focus()
    ta.setSelectionRange(caret, caret)
    refreshCursorLine()
  })
}

const PAIRS = { '(': ')', '[': ']', '{': '}', '"': '"', "'": "'", '`': '`' }
function handleAutoPair(e) {
  // 跳过：组合键 / IME / Ctrl/Alt/Meta
  if (e.ctrlKey || e.metaKey || e.altKey || e.isComposing) return
  const ta = editorRef.value
  if (!ta) return
  const k = e.key
  // 输入开括号 → 补成对 + 光标居中
  if (e.type === 'keydown' && Object.prototype.hasOwnProperty.call(PAIRS, k)) {
    const start = ta.selectionStart
    const end = ta.selectionEnd
    if (start !== end) {
      // 选区包起来
      e.preventDefault()
      const sel = ta.value.substring(start, end)
      const before = ta.value.substring(0, start)
      const after = ta.value.substring(end)
      const close = PAIRS[k]
      const newVal = before + k + sel + close + after
      emit('update:modelValue', newVal)
      nextTick(() => {
        ta.focus()
        ta.setSelectionRange(start + 1, end + 1)
        refreshCursorLine()
      })
    } else {
      e.preventDefault()
      const before = ta.value.substring(0, start)
      const after = ta.value.substring(start)
      const close = PAIRS[k]
      const newVal = before + k + close + after
      emit('update:modelValue', newVal)
      nextTick(() => {
        ta.focus()
        ta.setSelectionRange(start + 1, start + 1)
        refreshCursorLine()
      })
    }
  }
}

function onKeyup() {
  refreshCursorLine()
  updateCompletion()
}

function onBlur() {
  setTimeout(() => {
    completion.visible = false
  }, 120)
}

// 联想补全的详情面板定位（与弹窗右侧对齐）
const docStyle = computed(() => {
  const top = completion.style.top
  const left = completion.style.left
  // 解析 px
  const l = parseInt(String(left).replace('px', ''), 10) || 0
  const t = parseInt(String(top).replace('px', ''), 10) || 0
  // 弹窗宽度估算 320
  return { left: (l + 328) + 'px', top }
})

// kind 类别对应字母图标（VSCode 用单字母 + 颜色区分）
function kindIcon(kind) {
  switch (kind) {
    case 'keyword': return 'K'
    case 'type': return 'T'
    case 'function':
    case 'method': return 'ƒ'
    case 'variable':
    case 'var': return 'v'
    case 'value': return 'L'
    case 'property': return 'p'
    case 'tag': return 'T'
    case 'directive': return 'd'
    case 'block': return 'b'
    case 'operator':
    case 'mod': return 'o'
    case 'snippet': return '★'
    default: return '·'
  }
}

// onMounted 后把 pendingStops 推入 stopStack 供 Tab 切换
watch(pendingStops, (v) => {
  if (v && v.length) {
    stopStack.length = 0
    // 按 idx 排序：1, 2, 3... 最后 $0（idx=0）
    const sorted = [...v].sort((a, b) => {
      if (a.idx === 0) return 1
      if (b.idx === 0) return -1
      return a.idx - b.idx
    })
    stopStack.push(...sorted)
  }
}, { deep: true })

// 暴露方法给父组件（FileEditor 通过 ref 调用）。这些是占位实现，查找/替换等已迁移到 FileEditor 自身
// 的 findbar，编辑器核心是 modelValue 双向绑定和 Tab 缩进/联想。
defineExpose({
  findAll: () => 0,
  getMatchInfo: () => ({ count: 0, index: -1 }),
  findNext: () => {},
  findPrev: () => {},
  replaceCurrent: () => false,
  replaceAll: () => 0,
  setErrorLines: () => {},
  gotoLine: () => {}
})
</script>

<style scoped>
.nginx-editor {
  position: relative;
  border: 1px solid #1a1a1a;
  border-radius: 4px;
  background: #1e1e1e;
  height: 460px;
  overflow: hidden;
}
.nginx-editor.fill {
  height: 100%;
  border: none;
  border-radius: 0;
}
/* 行号 gutter（VSCode 风格：行号列用比编辑器稍亮的色，形成清晰边界） */
.ce-gutter {
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 50px;
  overflow: hidden;
  background: #252526;
  border-right: 1px solid #1a1a1a;
  z-index: 1;
  padding: 12px 0;
  box-sizing: border-box;
  text-align: right;
  user-select: none;
  pointer-events: none;
}
.ce-gutter-line {
  padding-right: 14px;
  font-family: Consolas, Menlo, "Courier New", monospace;
  font-size: 12px;
  line-height: 1.55;
  color: #858585;
  height: 1.55em;
  box-sizing: border-box;
}
.ce-gutter-line.active {
  color: #d4d4d4;
  background: #2a2a2a;
}
.nginx-highlight,
.nginx-textarea {
  position: absolute;
  inset: 0;
  margin: 0;
  border: 0;
  padding: 12px 14px 12px 64px;
  font-family: Consolas, Menlo, "Courier New", monospace;
  font-size: 13px;
  line-height: 1.55;
  white-space: pre;
  word-wrap: normal;
  overflow: auto;
  box-sizing: border-box;
  tab-size: 2;
  -moz-tab-size: 2;
}
.nginx-highlight {
  color: #d4d4d4;
  pointer-events: none;
  background: transparent;
  z-index: 1;
  overflow: hidden;
}
.nginx-textarea {
  color: transparent;
  caret-color: #fff;
  background: transparent;
  resize: none;
  outline: none;
  z-index: 2;
}
.nginx-textarea::selection { background: #264f78; color: transparent; }

/* 联想词弹窗（贴光标） */
.nginx-complete {
  position: absolute;
  z-index: 10;
  background: #252526;
  border: 1px solid #454545;
  border-radius: 4px;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.55);
  min-width: 280px;
  max-width: 380px;
  max-height: 240px;
  overflow-y: auto;
  font-family: Consolas, Menlo, monospace;
  font-size: 13px;
  padding: 4px 0;
}
.nginx-complete::-webkit-scrollbar { width: 6px; }
.nginx-complete::-webkit-scrollbar-thumb { background: #525252; border-radius: 3px; }
.nginx-complete-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 10px;
  cursor: pointer;
  color: #d4d4d4;
  line-height: 1.4;
}
.nginx-complete-item.active,
.nginx-complete-item:hover { background: #094771; color: #fff; }
.nginx-complete-item .nc-kind {
  width: 18px;
  height: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 700;
  border-radius: 3px;
  flex: none;
  color: #fff;
  font-style: italic;
}
.nginx-complete-item .nc-kind[data-kind="keyword"] { background: #569cd6; }
.nginx-complete-item .nc-kind[data-kind="type"] { background: #4ec9b0; }
.nginx-complete-item .nc-kind[data-kind="function"] { background: #dcdcaa; color: #1e1e1e; }
.nginx-complete-item .nc-kind[data-kind="method"] { background: #dcdcaa; color: #1e1e1e; }
.nginx-complete-item .nc-kind[data-kind="variable"],
.nginx-complete-item .nc-kind[data-kind="var"] { background: #9cdcfe; color: #1e1e1e; }
.nginx-complete-item .nc-kind[data-kind="value"] { background: #b5cea8; color: #1e1e1e; }
.nginx-complete-item .nc-kind[data-kind="property"] { background: #9cdcfe; color: #1e1e1e; }
.nginx-complete-item .nc-kind[data-kind="tag"] { background: #569cd6; }
.nginx-complete-item .nc-kind[data-kind="directive"] { background: #c586c0; }
.nginx-complete-item .nc-kind[data-kind="block"] { background: #c586c0; }
.nginx-complete-item .nc-kind[data-kind="operator"],
.nginx-complete-item .nc-kind[data-kind="mod"] { background: #d4d4d4; color: #1e1e1e; }
.nginx-complete-item .nc-kind[data-kind="snippet"] { background: #f9c74f; color: #1e1e1e; }
.nginx-complete-item .nc-label {
  font-weight: 600;
  color: #9cdcfe;
  min-width: 110px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nginx-complete-item:hover .nc-label,
.nginx-complete-item.active .nc-label { color: #fff; }
.nginx-complete-item .nc-desc { color: #9d9d9d; font-size: 12px; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.nginx-complete-item:hover .nc-desc,
.nginx-complete-item.active .nc-desc { color: rgba(255, 255, 255, 0.85); }

/* 详情面板（弹窗右侧） */
.nginx-complete-doc {
  position: absolute;
  z-index: 11;
  background: #1e1e1e;
  border: 1px solid #454545;
  border-radius: 4px;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.55);
  padding: 10px 12px;
  min-width: 220px;
  max-width: 320px;
  font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif;
  font-size: 12px;
  color: #d4d4d4;
}
.nginx-complete-doc .ncd-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}
.nginx-complete-doc .ncd-kind {
  width: 18px;
  height: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 700;
  border-radius: 3px;
  color: #fff;
  font-style: italic;
  font-family: Consolas, Menlo, monospace;
}
.nginx-complete-doc .ncd-kind[data-kind="keyword"] { background: #569cd6; }
.nginx-complete-doc .ncd-kind[data-kind="type"] { background: #4ec9b0; }
.nginx-complete-doc .ncd-kind[data-kind="function"] { background: #dcdcaa; color: #1e1e1e; }
.nginx-complete-doc .ncd-kind[data-kind="method"] { background: #dcdcaa; color: #1e1e1e; }
.nginx-complete-doc .ncd-kind[data-kind="variable"],
.nginx-complete-doc .ncd-kind[data-kind="var"] { background: #9cdcfe; color: #1e1e1e; }
.nginx-complete-doc .ncd-kind[data-kind="value"] { background: #b5cea8; color: #1e1e1e; }
.nginx-complete-doc .ncd-kind[data-kind="property"] { background: #9cdcfe; color: #1e1e1e; }
.nginx-complete-doc .ncd-kind[data-kind="tag"] { background: #569cd6; }
.nginx-complete-doc .ncd-kind[data-kind="directive"] { background: #c586c0; }
.nginx-complete-doc .ncd-kind[data-kind="block"] { background: #c586c0; }
.nginx-complete-doc .ncd-kind[data-kind="operator"],
.nginx-complete-doc .ncd-kind[data-kind="mod"] { background: #d4d4d4; color: #1e1e1e; }
.nginx-complete-doc .ncd-kind[data-kind="snippet"] { background: #f9c74f; color: #1e1e1e; }
.nginx-complete-doc .ncd-label {
  font-weight: 600;
  color: #9cdcfe;
  font-family: Consolas, Menlo, monospace;
  font-size: 13px;
}
.nginx-complete-doc .ncd-desc { color: #cccccc; line-height: 1.5; }
.nginx-complete-doc .ncd-tip {
  margin-top: 6px;
  padding-top: 6px;
  border-top: 1px solid #333;
  color: #858585;
  font-size: 11px;
}

/* ===== 移动端适配（<768px） ===== */
@media (max-width: 767px) {
  /* 关键：iOS Safari 聚焦 <16px 字体输入框会自动缩放页面，移动端强制 16px */
  .nginx-textarea { font-size: 16px; }
  .nginx-highlight { font-size: 16px; }
  .ce-gutter-line { font-size: 14px; }
  /* 行号 gutter 收窄，给代码区腾空间 */
  .ce-gutter { width: 40px; }
  .nginx-highlight,
  .nginx-textarea { padding-left: 50px; }
  /* 联想弹窗：限制在屏宽内，不超右边界 */
  .nginx-complete {
    min-width: 0;
    max-width: calc(100vw - 24px);
  }
  /* 详情面板：移动端空间不足，改为在弹窗下方（简化处理：隐藏，靠 desc 说明） */
  .nginx-complete-doc { display: none; }
}
</style>

<!-- 高亮 span 不带 scoped data-v，需全局样式（One Dark / VSCode Dark+ 配色） -->
<style>
/* nginx 手写高亮 + highlight.js 共用类名 */
.hl-comment { color: #6a9955; font-style: italic; }
.hl-quote { color: #6a9955; font-style: italic; }
.hl-keyword { color: #569cd6; font-weight: 600; }
.hl-string { color: #ce9178; }
.hl-number { color: #b5cea8; }
.hl-literal { color: #569cd6; }
.hl-regexp { color: #d16969; }
.hl-regex { color: #d16969; font-weight: 600; }
.hl-var { color: #9cdcfe; }
.hl-variable { color: #9cdcfe; }
.hl-path { color: #dcdcaa; }
.hl-ip { color: #4ec9b0; }
.hl-brace { color: #ffd700; font-weight: 700; }
.hl-punctuation { color: #d4d4d4; }
.hl-operator { color: #d4d4d4; }
.hl-title { color: #4ec9b0; }
.hl-title.function_ { color: #dcdcaa; }
.hl-title.class_ { color: #4ec9b0; }
.hl-function { color: #dcdcaa; }
.hl-built_in { color: #4ec9b0; }
.hl-type { color: #4ec9b0; }
.hl-params { color: #d4d4d4; }
.hl-attr { color: #9cdcfe; }
.hl-attribute { color: #9cdcfe; }
.hl-selector-tag { color: #569cd6; }
.hl-selector-class { color: #dcdcaa; }
.hl-selector-id { color: #dcdcaa; }
.hl-selector-attr { color: #9cdcfe; }
.hl-selector-pseudo { color: #569cd6; }
.hl-name { color: #d4d4d4; }
.hl-tag { color: #569cd6; }
.hl-meta { color: #9cdcfe; }
.hl-doctag { color: #c586c0; }
.hl-section { color: #569cd6; }
.hl-symbol { color: #b5cea8; }
.hl-bullet { color: #b5cea8; }
.hl-emphasis { font-style: italic; }
.hl-strong { font-weight: 700; }
.hl-addition { color: #b5cea8; }
.hl-deletion { color: #ce9178; }
.hl-code { color: #ce9178; }
.hl-template-tag { color: #569cd6; }
.hl-template-variable { color: #9cdcfe; }
</style>
