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
  html, body { margin: 0; padding: 0; height: 100%; width: 100%; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
    background: linear-gradient(160deg, #f2f5fa 0%, #e4eaf3 100%);
    color: #333; overflow: hidden;
  }
  /* 全屏舞台：flex 水平垂直居中（兼容性最高；用 inset 撑满避免滚动条致 100vw 偏右） */
  .stage {
    position: fixed; inset: 0; width: auto; height: auto;
    display: flex; align-items: center; justify-content: center;
  }
  .wrap {
    width: min(340px, calc(100vw - 32px)); flex: 0 0 auto;
    background: #fff; border-radius: 14px;
    box-shadow: 0 12px 40px rgba(30,50,90,.18);
    border: 1px solid rgba(30,50,90,.06);
    padding-bottom: 6px;
  }
  .cap-head {
    display: flex; align-items: center; gap: 8px;
    padding: 16px 18px 12px; font-size: 15px; font-weight: 600; color: #1f2b3d;
  }
  .cap-head .shield {
    width: 24px; height: 24px; flex: 0 0 24px; border-radius: 7px;
    background: linear-gradient(135deg,#409eff,#5b7cfa);
    display: flex; align-items: center; justify-content: center;
  }
  .cap-head .shield svg { width: 14px; height: 14px; }
  .cap-head small { font-weight: 400; color: #9aa6b6; font-size: 12px; margin-left: auto; white-space: nowrap; }

  .cap-box {
    position: relative; margin: 0 18px; border-radius: 10px; overflow: hidden;
    background-size: cover; background-position: center; user-select: none;
    border: 1px solid #e8edf4;
  }
  .cap-piece {
    position: absolute; top: 0; left: 0;
    filter: drop-shadow(0 4px 8px rgba(0,0,0,.3));
    cursor: grab; user-select: none; touch-action: none; opacity: .92;
    transition: opacity .2s;
  }
  .cap-piece.on { opacity: 1; }
  .cap-loading {
    position: absolute; inset: 0; display: flex; flex-direction: column; gap: 10px;
    align-items: center; justify-content: center;
    background: rgba(255,255,255,.82); color: #7a8798; font-size: 13px; z-index: 2;
  }
  .cap-loading .spin {
    width: 26px; height: 26px; border-radius: 50%;
    border: 3px solid #e8eef6; border-top-color: #409eff;
    animation: capspin .8s linear infinite;
  }
  @keyframes capspin { to { transform: rotate(360deg); } }

  .cap-bar { position: relative; margin: 14px 18px 0; height: 42px; }
  .cap-track {
    position: absolute; inset: 0;
    background: #eef2f7; border: 1px solid #dfe6ef; border-radius: 21px; overflow: hidden;
  }
  .cap-fill {
    position: absolute; left: 0; top: 0; bottom: 0; width: 0;
    background: linear-gradient(90deg, #79b7ff, #409eff);
    border-radius: 21px; transition: width .06s linear;
  }
  .cap-handle {
    position: absolute; top: 2px; left: 2px; width: 38px; height: 38px;
    background: #fff; border: 1px solid #c9d6e4; border-radius: 50%;
    z-index: 2; display: flex; align-items: center; justify-content: center;
    color: #409eff; cursor: grab; touch-action: none;
    box-shadow: 0 3px 8px rgba(50,80,120,.2);
    transition: transform .12s, box-shadow .12s;
  }
  .cap-handle:active { cursor: grabbing; transform: scale(1.05); box-shadow: 0 5px 14px rgba(50,80,120,.28); }
  .cap-handle svg { width: 20px; height: 20px; }
  .cap-tip {
    position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
    gap: 6px; font-size: 13px; pointer-events: none; z-index: 1; color: #7a8798;
    transition: color .2s; letter-spacing: 1px;
  }
  .cap-tip.ok { color: #34c77b; }
  .cap-tip.fail { color: #f56c6c; }

  .cap-foot {
    display: flex; align-items: center; justify-content: space-between;
    margin: 8px 18px 12px;
  }
  .cap-hint { font-size: 11px; color: #a6b0bf; }
  .cap-refresh {
    width: 32px; height: 32px; border-radius: 50%;
    background: #f2f5fa; color: #7a8798;
    display: flex; align-items: center; justify-content: center; cursor: pointer;
    transition: background .15s, color .15s, transform .15s;
  }
  .cap-refresh svg { width: 15px; height: 15px; }
  .cap-refresh:hover { background: #e8eef6; color: #409eff; }
  .cap-refresh:active { transform: rotate(180deg); }

  .wrap.done { animation: capdone .4s ease forwards; pointer-events: none; }
  @keyframes capdone {
    0% { transform: scale(1); opacity: 1; }
    100% { transform: scale(1.03); opacity: 0; }
  }
  @media (prefers-reduced-motion: reduce) {
    * { animation: none !important; transition: none !important; }
  }
</style>
</head>
<body>
  <div class="stage">
  <div class="wrap" id="wrap">
    <div class="cap-head">
      <span class="shield"><svg viewBox="0 0 24 24" fill="#fff"><path d="M12 1l9 4v6c0 5.5-3.8 10.7-9 12-5.2-1.3-9-6.5-9-12V5l9-4zm-1 15.5l6.5-6.5-1.4-1.4L11 13.7 8.4 11.1 7 12.5l4 4z"/></svg></span>
      安全验证
      <small>拖动滑块完成拼图</small>
    </div>
    <div class="cap-box" id="box">
      <img class="cap-piece" id="piece" draggable="false" alt="">
      <div class="cap-loading" id="loading"><div class="spin"></div><span>加载中…</span></div>
    </div>
    <div class="cap-bar">
      <div class="cap-track" id="track">
        <div class="cap-tip" id="tip">向右拖动滑块完成拼图</div>
        <div class="cap-fill" id="fill"></div>
      </div>
      <div class="cap-handle" id="handle" role="slider" aria-label="拖动滑块完成拼图" aria-valuemin="0" aria-valuemax="100" aria-valuenow="0">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">
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
    setTip('向右拖动滑块完成拼图');
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
        setTip('验证通过', 'ok');
        wrap.classList.add('done');
        setTimeout(function () { location.reload(); }, 420);
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
