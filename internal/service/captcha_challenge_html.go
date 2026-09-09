package service

// SiteCaptchaChallengeHTML 是拖拽验证码挑战页（由面板托管，经站点 nginx 同域反代暴露给访客）。
// 纯原生 HTML/JS，无框架依赖；先尝试站点 nginx 暴露的 /_lp_captcha_api/，失败回退面板直连 /api/。
const SiteCaptchaChallengeHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">
<title>安全验证</title>
<style>
  * { box-sizing: border-box; -webkit-tap-highlight-color: transparent; -webkit-user-select: none; -moz-user-select: none; user-select: none; }
  html, body { margin: 0; height: 100%; }
  body {
    display: flex; align-items: center; justify-content: center;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
    background: radial-gradient(1200px 800px at 20% 0%, #2c3a5e 0%, #171c2b 55%, #0f1420 100%);
    color: #e8ecf3; overflow: hidden;
  }
  /* 漂浮光斑背景 */
  body::before, body::after {
    content: ''; position: fixed; border-radius: 50%; filter: blur(80px); opacity: .35; z-index: 0;
  }
  body::before { width: 320px; height: 320px; background: #4f8cff; top: -90px; left: -60px; }
  body::after { width: 260px; height: 260px; background: #7c5cff; bottom: -80px; right: -50px; }

  .wrap {
    position: relative; z-index: 1;
    width: 340px; max-width: calc(100vw - 28px);
    background: rgba(255,255,255,.08);
    border: 1px solid rgba(255,255,255,.14);
    border-radius: 18px; overflow: hidden;
    box-shadow: 0 24px 70px rgba(0,0,0,.5), inset 0 1px 0 rgba(255,255,255,.12);
    backdrop-filter: blur(18px); -webkit-backdrop-filter: blur(18px);
  }
  .cap-head {
    display: flex; align-items: center; gap: 8px;
    padding: 16px 18px 12px; font-size: 16px; font-weight: 600; color: #fff;
  }
  .cap-head .shield {
    width: 26px; height: 26px; flex: 0 0 26px; border-radius: 8px;
    background: linear-gradient(135deg,#4f8cff,#7c5cff);
    display: flex; align-items: center; justify-content: center; font-size: 14px;
    box-shadow: 0 4px 12px rgba(79,140,255,.35);
  }
  .cap-head small { font-weight: 400; color: rgba(255,255,255,.5); font-size: 12px; margin-left: auto; }

  .cap-box {
    position: relative; margin: 0 18px; border-radius: 12px; overflow: hidden;
    background-size: cover; background-position: center; user-select: none;
    box-shadow: inset 0 0 0 1px rgba(255,255,255,.1);
  }
  .cap-piece {
    position: absolute; top: 0; left: 0;
    filter: drop-shadow(0 3px 6px rgba(0,0,0,.35));
    cursor: grab; user-select: none; touch-action: none;
    opacity: .94; transition: opacity .2s;
  }
  .cap-piece.on { opacity: 1; }
  .cap-loading {
    position: absolute; inset: 0; display: flex; flex-direction: column; gap: 10px;
    align-items: center; justify-content: center;
    background: rgba(20,24,38,.72); color: #aeb6c8; font-size: 13px; z-index: 2;
  }
  .cap-loading .spin {
    width: 28px; height: 28px; border-radius: 50%;
    border: 3px solid rgba(255,255,255,.15); border-top-color: #7c9cff;
    animation: capspin .8s linear infinite;
  }
  @keyframes capspin { to { transform: rotate(360deg); } }

  .cap-bar { position: relative; margin: 14px 18px 0; height: 46px; }
  .cap-track {
    position: absolute; inset: 0;
    background: rgba(255,255,255,.06);
    border: 1px solid rgba(255,255,255,.14);
    border-radius: 23px; overflow: hidden;
  }
  .cap-fill {
    position: absolute; left: 0; top: 0; bottom: 0; width: 0;
    background: linear-gradient(90deg, #4f8cff, #6fb1ff);
    border-radius: 23px; opacity: .85; transition: width .06s linear;
  }
  .cap-handle {
    position: absolute; top: 2px; left: 2px; width: 42px; height: 42px;
    background: linear-gradient(135deg,#fff,#eef1f7);
    border-radius: 50%; z-index: 2;
    display: flex; align-items: center; justify-content: center;
    color: #4f8cff; cursor: grab; touch-action: none;
    box-shadow: 0 3px 10px rgba(0,0,0,.25);
    transition: transform .15s, box-shadow .15s;
  }
  .cap-handle:active { cursor: grabbing; transform: scale(1.06); box-shadow: 0 5px 16px rgba(0,0,0,.35); }
  .cap-handle svg { width: 20px; height: 20px; }
  .cap-tip {
    position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
    gap: 6px; font-size: 13px; pointer-events: none; z-index: 1;
    color: rgba(255,255,255,.55); transition: color .2s;
  }
  .cap-tip.ok { color: #4cd98a; }
  .cap-tip.fail { color: #ff6b6b; }

  .cap-foot {
    display: flex; align-items: center; justify-content: space-between;
    margin: 8px 18px 14px;
  }
  .cap-hint { font-size: 11px; color: rgba(255,255,255,.35); }
  .cap-refresh {
    width: 34px; height: 34px; border-radius: 50%;
    background: rgba(255,255,255,.08); color: rgba(255,255,255,.7);
    display: flex; align-items: center; justify-content: center; cursor: pointer;
    border: 1px solid rgba(255,255,255,.12);
    transition: background .15s, transform .15s;
  }
  .cap-refresh svg { width: 16px; height: 16px; }
  .cap-refresh:hover { background: rgba(255,255,255,.15); }
  .cap-refresh:active { transform: rotate(180deg); }

  /* 成功状态：整个卡片浮起淡出 */
  .wrap.done {
    animation: capdone .45s ease forwards;
    pointer-events: none;
  }
  @keyframes capdone {
    0% { transform: scale(1); opacity: 1; }
    100% { transform: scale(1.04); opacity: 0; }
  }
  @media (prefers-reduced-motion: reduce) {
    * { animation: none !important; transition: none !important; }
  }
</style>
</head>
<body>
  <div class="wrap" id="wrap">
    <div class="cap-head">
      <span class="shield">🛡</span>
      安全验证
      <small>拖动滑块完成拼图</small>
    </div>
    <div class="cap-box" id="box">
      <img class="cap-piece" id="piece" draggable="false" alt="">
      <div class="cap-loading" id="loading"><div class="spin"></div><span>加载中…</span></div>
    </div>
    <div class="cap-bar">
      <div class="cap-track" id="track">
        <div class="cap-tip" id="tip">按住滑块向右拖动</div>
        <div class="cap-fill" id="fill"></div>
      </div>
      <div class="cap-handle" id="handle" role="slider" aria-label="拖动滑块完成拼图" aria-valuemin="0" aria-valuemax="100" aria-valuenow="0">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M4 12h14M13 5l7 7-7 7"/>
        </svg>
      </div>
    </div>
    <div class="cap-foot">
      <span class="cap-hint">请将拼图拖到缺口处</span>
      <div class="cap-refresh" id="refresh" title="换一张" role="button" aria-label="换一张">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M21 12a9 9 0 1 1-3-6.7"/><path d="M21 3v6h-6"/>
        </svg>
      </div>
    </div>
  </div>
<script>
(function () {
  // site 由面板 /captcha-challenge 从自身 query 注入（__LP_SITE__ 占位符），
  // 不依赖浏览器 URL 的 search——因为 error_page 401 内部重定向时浏览器地址栏仍是原路径，
  // ?site=xxx 只存在于 nginx 内部 proxy_pass 目标里，location.search 取不到会导致 site=0 → 加载失败。
  var site = '__LP_SITE__' || '0';
  var wrap = document.getElementById('wrap');
  var box = document.getElementById('box');
  var piece = document.getElementById('piece');
  var loading = document.getElementById('loading');
  var track = document.getElementById('track');
  var fill = document.getElementById('fill');
  var handle = document.getElementById('handle');
  var tip = document.getElementById('tip');
  var refresh = document.getElementById('refresh');
  var cur = null;        // { token, target_y, piece_size, width, height, bg, piece }（不含横向答案 target_x）
  var dragging = false;
  var startX = 0, startLeft = 0;

  function apiTry(u) {
    return fetch(u).then(function (r) { return r.json(); }).catch(function () { return null; });
  }
  function puzzleURL() { return '/_lp_captcha_api/site/captcha/puzzle?site=' + site; }
  function puzzleURLFallback() { return '/api/site/captcha/puzzle?site=' + site; }
  function verifyURL() { return '/_lp_captcha_api/site/captcha/verify'; }
  function verifyURLFallback() { return '/api/site/captcha/verify'; }

  function setTip(text, cls) {
    tip.textContent = text;
    tip.className = 'cap-tip' + (cls ? ' ' + cls : '');
  }

  function loadPuzzle() {
    loading.style.display = 'flex';
    setTip('按住滑块向右拖动');
    piece.classList.remove('on');
    apiTry(puzzleURL()).then(function (d) {
      if (!d || !d.token) return apiTry(puzzleURLFallback());
      return d;
    }).then(function (d) {
      if (!d || !d.token) { setTip('验证码加载失败，请点刷新重试', 'fail'); loading.style.display = 'none'; return; }
      cur = d;
      box.style.width = d.width + 'px';
      box.style.height = d.height + 'px';
      box.style.backgroundImage = 'url(' + d.bg + ')';
      piece.src = d.piece;
      piece.style.width = d.piece_size + 'px';
      piece.style.height = d.piece_size + 'px';
      piece.style.top = d.target_y + 'px';
      piece.style.left = '0px';
      setHandle(0);
      handle.setAttribute('aria-valuemax', String(cur.width - cur.piece_size));
      handle.setAttribute('aria-valuenow', '0');
      loading.style.display = 'none';
      requestAnimationFrame(function () { piece.classList.add('on'); });
    });
  }

  function maxX() { return cur ? (cur.width - cur.piece_size) : 0; }
  function trackW() { return track.clientWidth - handle.clientWidth; }

  function setHandle(px) {
    if (!cur) return;
    px = Math.max(0, Math.min(px, trackW()));
    handle.style.left = px + 'px';
    fill.style.width = px + 'px';
    handle.setAttribute('aria-valuenow', String(Math.round(px)));
    var ratio = trackW() > 0 ? px / trackW() : 0;
    piece.style.left = Math.round(ratio * maxX()) + 'px';
  }

  function onDown(e) {
    if (!cur) return;
    dragging = true;
    startX = (e.touches ? e.touches[0].clientX : e.clientX);
    startLeft = parseFloat(handle.style.left) || 0;
    piece.classList.add('on');
    e.preventDefault();
  }
  function onMove(e) {
    if (!dragging) return;
    var x = (e.touches ? e.touches[0].clientX : e.clientX);
    setHandle(startLeft + (x - startX));
    e.preventDefault();
  }
  function onUp() {
    if (!dragging) return;
    dragging = false;
    var x = Math.round(parseFloat(piece.style.left) || 0);
    verify(x);
  }

  function verify(x) {
    var body = JSON.stringify({ token: cur.token, x: x });
    function doVerify(url) {
      return fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: body })
        .then(function (r) { return r.ok; }).catch(function () { return null; });
    }
    doVerify(verifyURL()).then(function (ok) {
      if (ok === null) return doVerify(verifyURLFallback());
      return ok;
    }).then(function (ok) {
      if (ok) {
        setTip('✓ 验证通过', 'ok');
        wrap.classList.add('done');
        setTimeout(function () { location.reload(); }, 480);
      } else {
        setTip('验证失败，请重试', 'fail');
        loadPuzzle();
      }
    });
  }

  handle.addEventListener('mousedown', onDown);
  handle.addEventListener('touchstart', onDown, { passive: false });
  document.addEventListener('mousemove', onMove);
  document.addEventListener('touchmove', onMove, { passive: false });
  document.addEventListener('mouseup', onUp);
  document.addEventListener('touchend', onUp);
  refresh.addEventListener('click', loadPuzzle);
  loadPuzzle();
})();
</script>
</body>
</html>`
