// 文件类型图标配色表 —— 自绘彩色 SVG 文件图标
// 每条 { body, fold, text, label }:
//   body: 文件主体色
//   fold: 右上折角色（略深）
//   text: 标签文字色（与 body 形成对比）
//   label: 显示在文件图标中央的缩写（一般取扩展名大写，最长 4 字符）
//
// 当扩展名未匹配时使用 FALLBACK_PALETTE（通用文件）。
// 文件夹使用 FOLDER_PALETTE（自带文件夹 SVG，由 FileTypeIcon 组件识别 is_dir 渲染）。

export const FALLBACK_PALETTE = { body: '#607d8b', fold: '#37474f', text: '#ffffff', label: 'FILE' }
export const FOLDER_PALETTE  = { body: '#fbc02d', fold: '#f9a825', text: '#ffffff', label: 'DIR'  }

export const FILE_PALETTE = {
  // ---------- 文本 ----------
  txt: { body: '#5b6b79', fold: '#3f4a55', text: '#ffffff', label: 'TXT' },
  md:  { body: '#083fa1', fold: '#062c73', text: '#ffffff', label: 'MD'  },
  log: { body: '#5b6b79', fold: '#3f4a55', text: '#ffffff', label: 'LOG' },
  rtf: { body: '#5b6b79', fold: '#3f4a55', text: '#ffffff', label: 'RTF' },

  // ---------- 配置 / 数据 ----------
  json: { body: '#fbbf24', fold: '#d97706', text: '#3a2410', label: 'JSON' },
  xml:  { body: '#ff6600', fold: '#cc4d00', text: '#ffffff', label: 'XML'  },
  yaml: { body: '#cb171e', fold: '#a01418', text: '#ffffff', label: 'YAML' },
  yml:  { body: '#cb171e', fold: '#a01418', text: '#ffffff', label: 'YML'  },
  toml: { body: '#9c4221', fold: '#7a3418', text: '#ffffff', label: 'TOML' },
  ini:  { body: '#6b7280', fold: '#4b5563', text: '#ffffff', label: 'INI'  },
  conf: { body: '#6b7280', fold: '#4b5563', text: '#ffffff', label: 'CONF' },
  env:  { body: '#ecd53f', fold: '#c0b32e', text: '#3a2410', label: 'ENV'  },
  csv:  { body: '#22a55c', fold: '#1c8049', text: '#ffffff', label: 'CSV'  },

  // ---------- 前端 ----------
  html:  { body: '#e44d26', fold: '#f16529', text: '#ffffff', label: 'HTML'  },
  htm:   { body: '#e44d26', fold: '#f16529', text: '#ffffff', label: 'HTM'   },
  xhtml: { body: '#e44d26', fold: '#f16529', text: '#ffffff', label: 'XHTML' },
  css:   { body: '#2965f1', fold: '#264de0', text: '#ffffff', label: 'CSS'   },
  scss:  { body: '#c69',    fold: '#a78',    text: '#ffffff', label: 'SCSS'  },
  less:  { body: '#1d365d', fold: '#29376e', text: '#ffffff', label: 'LESS'  },
  sass:  { body: '#a53b70', fold: '#7e2a52', text: '#ffffff', label: 'SASS'  },
  js:    { body: '#f7df1e', fold: '#d9bf1c', text: '#323232', label: 'JS'    },
  mjs:   { body: '#f7df1e', fold: '#d9bf1c', text: '#323232', label: 'MJS'   },
  cjs:   { body: '#f7df1e', fold: '#d9bf1c', text: '#323232', label: 'CJS'   },
  jsx:   { body: '#61dafb', fold: '#21a5cf', text: '#102a3a', label: 'JSX'   },
  ts:    { body: '#3178c6', fold: '#235a97', text: '#ffffff', label: 'TS'    },
  tsx:   { body: '#3178c6', fold: '#235a97', text: '#ffffff', label: 'TSX'   },
  vue:   { body: '#41b883', fold: '#35495e', text: '#ffffff', label: 'VUE'   },
  svelte:{ body: '#ff3e00', fold: '#cc3200', text: '#ffffff', label: 'SVL'   },

  // ---------- 后端 / 系统编程 ----------
  py:    { body: '#3776ab', fold: '#2a5d87', text: '#ffd43b', label: 'PY'    },
  go:    { body: '#00add8', fold: '#007d9c', text: '#ffffff', label: 'GO'    },
  java:  { body: '#f89820', fold: '#c97a18', text: '#ffffff', label: 'JAVA'  },
  kt:    { body: '#7f52ff', fold: '#5f39cc', text: '#ffffff', label: 'KT'    },
  scala: { body: '#dc322f', fold: '#a82322', text: '#ffffff', label: 'SCL'   },
  groovy:{ body: '#629629', fold: '#46731f', text: '#ffffff', label: 'GRV'   },
  c:     { body: '#283593', fold: '#1a237e', text: '#ffffff', label: 'C'     },
  h:     { body: '#283593', fold: '#1a237e', text: '#ffffff', label: 'H'     },
  cpp:   { body: '#00599c', fold: '#004482', text: '#ffffff', label: 'C++'   },
  cc:    { body: '#00599c', fold: '#004482', text: '#ffffff', label: 'CC'    },
  cxx:   { body: '#00599c', fold: '#004482', text: '#ffffff', label: 'CXX'   },
  hpp:   { body: '#00599c', fold: '#004482', text: '#ffffff', label: 'H++'   },
  cs:    { body: '#9b4f96', fold: '#7a3f7a', text: '#ffffff', label: 'C#'    },
  fs:    { body: '#30b9db', fold: '#2596b3', text: '#ffffff', label: 'F#'    },
  vb:    { body: '#945199', fold: '#744277', text: '#ffffff', label: 'VB'    },
  php:   { body: '#777bb4', fold: '#4f5b99', text: '#ffffff', label: 'PHP'   },
  rb:    { body: '#cc342d', fold: '#a32924', text: '#ffffff', label: 'RB'    },
  rails: { body: '#cc0000', fold: '#a30000', text: '#ffffff', label: 'RAILS' },
  rs:    { body: '#dea584', fold: '#a77253', text: '#3a2410', label: 'RS'    },
  swift: { body: '#f05138', fold: '#bf4128', text: '#ffffff', label: 'SWIFT' },

  // ---------- Shell / 脚本 ----------
  sh:   { body: '#2d2d2d', fold: '#000000', text: '#89e051', label: 'SH'   },
  bash: { body: '#2d2d2d', fold: '#000000', text: '#89e051', label: 'BASH' },
  zsh:  { body: '#2d2d2d', fold: '#000000', text: '#89e051', label: 'ZSH'  },
  fish: { body: '#2d2d2d', fold: '#000000', text: '#89e051', label: 'FISH' },
  ps1:  { body: '#012456', fold: '#001b3a', text: '#ffffff', label: 'PS1'  },
  bat:  { body: '#3a3a3a', fold: '#1f1f1f', text: '#ffffff', label: 'BAT'  },
  cmd:  { body: '#3a3a3a', fold: '#1f1f1f', text: '#ffffff', label: 'CMD'  },

  // ---------- 数据库 ----------
  sql:     { body: '#e38c00', fold: '#b46f00', text: '#ffffff', label: 'SQL'    },
  db:      { body: '#3b6ea5', fold: '#2d5783', text: '#ffffff', label: 'DB'     },
  sqlite:  { body: '#0064a1', fold: '#004e7e', text: '#ffffff', label: 'SQLITE' },
  sqlite3: { body: '#0064a1', fold: '#004e7e', text: '#ffffff', label: 'SQLIT3' },

  // ---------- 图片 ----------
  jpg:  { body: '#7e57c2', fold: '#5c41a3', text: '#ffffff', label: 'JPG'  },
  jpeg: { body: '#7e57c2', fold: '#5c41a3', text: '#ffffff', label: 'JPEG' },
  png:  { body: '#26a69a', fold: '#1d8276', text: '#ffffff', label: 'PNG'  },
  gif:  { body: '#ec407a', fold: '#bc2d5e', text: '#ffffff', label: 'GIF'  },
  webp: { body: '#42a5f5', fold: '#3084c4', text: '#ffffff', label: 'WEBP' },
  bmp:  { body: '#26a69a', fold: '#1d8276', text: '#ffffff', label: 'BMP'  },
  svg:  { body: '#ff9800', fold: '#cc7a00', text: '#ffffff', label: 'SVG'  },
  ico:  { body: '#3b82f6', fold: '#2864c4', text: '#ffffff', label: 'ICO'  },
  tiff: { body: '#7e57c2', fold: '#5c41a3', text: '#ffffff', label: 'TIFF' },
  tif:  { body: '#7e57c2', fold: '#5c41a3', text: '#ffffff', label: 'TIF'  },
  heic: { body: '#7e57c2', fold: '#5c41a3', text: '#ffffff', label: 'HEIC' },
  avif: { body: '#7e57c2', fold: '#5c41a3', text: '#ffffff', label: 'AVIF' },

  // ---------- 视频 ----------
  mp4:  { body: '#e91e63', fold: '#b8184c', text: '#ffffff', label: 'MP4'  },
  webm: { body: '#e91e63', fold: '#b8184c', text: '#ffffff', label: 'WEBM' },
  mov:  { body: '#e91e63', fold: '#b8184c', text: '#ffffff', label: 'MOV'  },
  avi:  { body: '#e91e63', fold: '#b8184c', text: '#ffffff', label: 'AVI'  },
  mkv:  { body: '#e91e63', fold: '#b8184c', text: '#ffffff', label: 'MKV'  },
  m4v:  { body: '#e91e63', fold: '#b8184c', text: '#ffffff', label: 'M4V'  },
  flv:  { body: '#e91e63', fold: '#b8184c', text: '#ffffff', label: 'FLV'  },
  wmv:  { body: '#e91e63', fold: '#b8184c', text: '#ffffff', label: 'WMV'  },
  rmvb: { body: '#e91e63', fold: '#b8184c', text: '#ffffff', label: 'RMVB' },

  // ---------- 音频 ----------
  mp3:  { body: '#ff7043', fold: '#cc5635', text: '#ffffff', label: 'MP3'  },
  wav:  { body: '#ff7043', fold: '#cc5635', text: '#ffffff', label: 'WAV'  },
  flac: { body: '#ff7043', fold: '#cc5635', text: '#ffffff', label: 'FLAC' },
  aac:  { body: '#ff7043', fold: '#cc5635', text: '#ffffff', label: 'AAC'  },
  m4a:  { body: '#ff7043', fold: '#cc5635', text: '#ffffff', label: 'M4A'  },
  ogg:  { body: '#ff7043', fold: '#cc5635', text: '#ffffff', label: 'OGG'  },
  oga:  { body: '#ff7043', fold: '#cc5635', text: '#ffffff', label: 'OGA'  },
  opus: { body: '#ff7043', fold: '#cc5635', text: '#ffffff', label: 'OPUS' },
  wma:  { body: '#ff7043', fold: '#cc5635', text: '#ffffff', label: 'WMA'  },

  // ---------- 压缩 ----------
  zip:   { body: '#8d6e63', fold: '#6e564f', text: '#ffffff', label: 'ZIP'  },
  rar:   { body: '#8d6e63', fold: '#6e564f', text: '#ffffff', label: 'RAR'  },
  '7z':  { body: '#8d6e63', fold: '#6e564f', text: '#ffffff', label: '7Z'   },
  tar:   { body: '#8d6e63', fold: '#6e564f', text: '#ffffff', label: 'TAR'  },
  gz:    { body: '#8d6e63', fold: '#6e564f', text: '#ffffff', label: 'GZ'   },
  tgz:   { body: '#8d6e63', fold: '#6e564f', text: '#ffffff', label: 'TGZ'  },
  bz2:   { body: '#8d6e63', fold: '#6e564f', text: '#ffffff', label: 'BZ2'  },
  xz:    { body: '#8d6e63', fold: '#6e564f', text: '#ffffff', label: 'XZ'   },
  zst:   { body: '#8d6e63', fold: '#6e564f', text: '#ffffff', label: 'ZST'  },

  // ---------- 文档 ----------
  pdf:  { body: '#d32f2f', fold: '#a52525', text: '#ffffff', label: 'PDF'  },
  doc:  { body: '#2b579a', fold: '#1e3e6f', text: '#ffffff', label: 'DOC'  },
  docx: { body: '#2b579a', fold: '#1e3e6f', text: '#ffffff', label: 'DOCX' },
  xls:  { body: '#1d7044', fold: '#155434', text: '#ffffff', label: 'XLS'  },
  xlsx: { body: '#1d7044', fold: '#155434', text: '#ffffff', label: 'XLSX' },
  ppt:  { body: '#c43e1c', fold: '#9a2f15', text: '#ffffff', label: 'PPT'  },
  pptx: { body: '#c43e1c', fold: '#9a2f15', text: '#ffffff', label: 'PPTX' },
  odt:  { body: '#1f91cf', fold: '#176d9d', text: '#ffffff', label: 'ODT'  },
  ods:  { body: '#1f91cf', fold: '#176d9d', text: '#ffffff', label: 'ODS'  },
  odp:  { body: '#1f91cf', fold: '#176d9d', text: '#ffffff', label: 'ODP'  },

  // ---------- 可执行 / 安装包 ----------
  exe:  { body: '#37474f', fold: '#222b30', text: '#80deea', label: 'EXE' },
  msi:  { body: '#37474f', fold: '#222b30', text: '#80deea', label: 'MSI' },
  deb:  { body: '#37474f', fold: '#222b30', text: '#fb8c00', label: 'DEB' },
  rpm:  { body: '#37474f', fold: '#222b30', text: '#fb8c00', label: 'RPM' },
  dmg:  { body: '#37474f', fold: '#222b30', text: '#80deea', label: 'DMG' },
  iso:  { body: '#37474f', fold: '#222b30', text: '#80deea', label: 'ISO' },
  apk:  { body: '#37474f', fold: '#222b30', text: '#a4c639', label: 'APK' },
  ipa:  { body: '#37474f', fold: '#222b30', text: '#80deea', label: 'IPA' },

  // ---------- 字体 ----------
  ttf:   { body: '#5d4037', fold: '#3e2723', text: '#ffffff', label: 'TTF'   },
  otf:   { body: '#5d4037', fold: '#3e2723', text: '#ffffff', label: 'OTF'   },
  woff:  { body: '#5d4037', fold: '#3e2723', text: '#ffffff', label: 'WOFF'  },
  woff2: { body: '#5d4037', fold: '#3e2723', text: '#ffffff', label: 'WOFF2' },
  eot:   { body: '#5d4037', fold: '#3e2723', text: '#ffffff', label: 'EOT'   },

  // ---------- 证书 / 密钥 ----------
  pem: { body: '#b71c1c', fold: '#8c1717', text: '#ffffff', label: 'PEM' },
  key: { body: '#b71c1c', fold: '#8c1717', text: '#ffffff', label: 'KEY' },
  crt: { body: '#b71c1c', fold: '#8c1717', text: '#ffffff', label: 'CRT' },
  cer: { body: '#b71c1c', fold: '#8c1717', text: '#ffffff', label: 'CER' },
  p12: { body: '#b71c1c', fold: '#8c1717', text: '#ffffff', label: 'P12' },
  pfx: { body: '#b71c1c', fold: '#8c1717', text: '#ffffff', label: 'PFX' },
  pub: { body: '#1565c0', fold: '#0d47a1', text: '#ffffff', label: 'PUB' }
}

/**
 * 取扩展名（不带点），统一小写；如果不存在则返回空串。
 */
export function fileExt(name) {
  if (!name) return ''
  // 以最后一个 . 划分
  const base = String(name)
  const i = base.lastIndexOf('.')
  if (i < 0 || i === base.length - 1) return ''
  return base.slice(i + 1).toLowerCase()
}

/**
 * 取一个文件/文件夹要用的配色对象。
 * isDir = true → 返回 FOLDER_PALETTE
 * 否则 → 查 FILE_PALETTE[ext]，未命中 FALLBACK_PALETTE
 *
 * 之所以保留旧导出名 fileIcon(name)，仅是为兼容（已不再被组件直接调用，
 * 但若其他第三方代码仍在 import，使用仍可工作）。
 */
export function fileIcon(name) {
  const ext = fileExt(name)
  return FILE_PALETTE[ext] || FALLBACK_PALETTE
}
