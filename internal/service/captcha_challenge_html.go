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
    background: linear-gradient(135deg, #1f2430 0%, #2b3142 100%); color: #e8ecf3;
  }
  .wrap {
    width: 340px; background: #fff; border-radius: 12px; overflow: hidden;
    box-shadow: 0 20px 60px rgba(0,0,0,.35); padding-bottom: 14px;
  }
  .cap-head { padding: 14px 16px 8px; font-size: 15px; color: #2b3142; font-weight: 600; }
  .cap-head small { font-weight: 400; color: #98a2b3; font-size: 12px; margin-left: 6px; }
  .cap-box {
    position: relative; margin: 0 16px; border-radius: 8px; overflow: hidden;
    background-size: cover; background-position: center; user-select: none;
  }
  .cap-piece {
    position: absolute; top: 0; left: 0; width: 42px; height: 42px;
    border: 1px solid rgba(0,0,0,.25); box-shadow: 0 2px 8px rgba(0,0,0,.35);
    cursor: grab; user-select: none; touch-action: none;
  }
  .cap-piece:active { cursor: grabbing; }
  .cap-loading {
    position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
    background: rgba(255,255,255,.7); color: #5b6577; font-size: 13px;
  }
  .cap-bar { position: relative; margin: 12px 16px 0; height: 40px; }
  .cap-track {
    position: absolute; inset: 0; background: #eef1f6; border: 1px solid #dfe4ec;
    border-radius: 20px; overflow: hidden;
  }
  .cap-fill {
    position: absolute; left: 0; top: 0; bottom: 0; width: 0;
    background: linear-gradient(90deg, #4f8cff, #6fb1ff); transition: width .05s linear;
  }
  .cap-handle {
    position: absolute; top: 0; left: 0; width: 40px; height: 40px;
    background: #fff; border: 1px solid #dfe4ec; border-radius: 50%;
    display: flex; align-items: center; justify-content: center;
    color: #4f8cff; font-size: 18px; cursor: grab; touch-action: none;
    box-shadow: 0 2px 6px rgba(0,0,0,.15);
  }
  .cap-handle:active { cursor: grabbing; }
  .cap-tip {
    position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
    font-size: 12px; color: #8a94a6; pointer-events: none;
  }
  .cap-foot { display: flex; justify-content: flex-end; margin: 8px 16px 0; }
  .cap-refresh {
    width: 30px; height: 30px; border-radius: 50%; background: #f3f5f9; color: #6b7585;
    display: flex; align-items: center; justify-content: center; cursor: pointer; font-size: 16px;
  }
  .cap-refresh:active { background: #e7ebf2; }
</style>
</head>
<body>
  <div class="wrap">
    <div class="cap-head">安全验证<small>拖动滑块完成拼图</small></div>
    <div class="cap-box" id="box">
      <img class="cap-piece" id="piece" draggable="false" alt="">
      <div class="cap-loading" id="loading">加载中…</div>
    </div>
    <div class="cap-bar">
      <div class="cap-track" id="track">
        <div class="cap-fill" id="fill"></div>
        <div class="cap-tip" id="tip">请按住滑块拖动</div>
      </div>
      <div class="cap-handle" id="handle">→</div>
    </div>
    <div class="cap-foot"><div class="cap-refresh" id="refresh" title="换一张">↻</div></div>
  </div>
<script>
(function () {
  // site 由面板 /captcha-challenge 从自身 query 注入（__LP_SITE__ 占位符），
  // 不依赖浏览器 URL 的 search——因为 error_page 401 内部重定向时浏览器地址栏仍是原路径，
  // ?site=xxx 只存在于 nginx 内部 proxy_pass 目标里，location.search 取不到会导致 site=0 → 加载失败。
  var site = '__LP_SITE__' || '0';
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

  function loadPuzzle() {
    loading.style.display = 'flex';
    tip.textContent = '请按住滑块拖动';
    apiTry(puzzleURL()).then(function (d) {
      if (!d || !d.token) return apiTry(puzzleURLFallback());
      return d;
    }).then(function (d) {
      if (!d || !d.token) { tip.textContent = '验证码加载失败，请点 ↻ 重试'; loading.style.display = 'none'; return; }
      cur = d;
      box.style.width = d.width + 'px';
      box.style.height = d.height + 'px';
      box.style.backgroundImage = 'url(' + d.bg + ')';
      piece.src = d.piece;
      piece.style.top = d.target_y + 'px';
      piece.style.left = '0px';
      setHandle(0);
      loading.style.display = 'none';
    });
  }

  function maxX() { return cur ? (cur.width - cur.piece_size) : 0; }
  function trackW() { return track.clientWidth - handle.clientWidth; }

  function setHandle(px) {
    if (!cur) return;
    px = Math.max(0, Math.min(px, trackW()));
    handle.style.left = px + 'px';
    fill.style.width = px + 'px';
    var ratio = trackW() > 0 ? px / trackW() : 0;
    piece.style.left = Math.round(ratio * maxX()) + 'px';
  }

  function onDown(e) {
    if (!cur) return;
    dragging = true;
    startX = (e.touches ? e.touches[0].clientX : e.clientX);
    startLeft = parseFloat(handle.style.left) || 0;
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
        tip.textContent = '验证成功，正在进入…';
        setTimeout(function () { location.reload(); }, 500);
      } else {
        tip.textContent = '验证失败，请重试';
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
