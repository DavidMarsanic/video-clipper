(function () {
  'use strict';

  var state = {
    url: '',
    duration: 0,
    start: 0,
    end: 0,
    mode: 'full',
    activeDragHandle: null,
    jobId: null,
    eventSource: null,
    lastPath: '',
  };

  var el = function (id) { return document.getElementById(id); };
  var clamp = function (v, lo, hi) { return Math.max(lo, Math.min(hi, v)); };

  // ---- timecode parsing/formatting ----------------------------------

  // Accepts "75", "1:15", "01:15.500", "1:02:15" — any of H:M:S / M:S /
  // S with an optional fractional seconds part.
  function parseTimecode(input) {
    if (input == null) return NaN;
    var s = String(input).trim();
    if (s === '') return NaN;
    var parts = s.split(':');
    if (parts.length > 3) return NaN;
    for (var i = 0; i < parts.length; i++) {
      if (!/^\d+(\.\d+)?$/.test(parts[i])) return NaN;
    }
    var seconds = 0;
    for (var j = 0; j < parts.length; j++) {
      seconds = seconds * 60 + parseFloat(parts[j]);
    }
    return seconds;
  }

  function pad2(n) { return String(n).padStart(2, '0'); }

  function formatField(seconds) {
    if (!isFinite(seconds) || seconds < 0) seconds = 0;
    var m = Math.floor(seconds / 60);
    var s = seconds - m * 60;
    return pad2(m) + ':' + s.toFixed(1).padStart(4, '0');
  }

  function formatLabel(seconds) {
    seconds = Math.max(0, Math.floor(seconds || 0));
    var h = Math.floor(seconds / 3600);
    var m = Math.floor((seconds % 3600) / 60);
    var s = seconds % 60;
    if (h > 0) return h + ':' + pad2(m) + ':' + pad2(s);
    return pad2(m) + ':' + pad2(s);
  }

  function debounce(fn, ms) {
    var t;
    return function () {
      var args = arguments;
      clearTimeout(t);
      t = setTimeout(function () { fn.apply(null, args); }, ms);
    };
  }

  function looksLikeUrl(v) {
    try {
      var u = new URL(v);
      return u.protocol === 'http:' || u.protocol === 'https:';
    } catch (e) {
      return false;
    }
  }

  // ---- networking ------------------------------------------------------

  function friendlyError(data) {
    var map = {
      unsupported: "That URL isn't supported.",
      drm: 'This video is DRM-protected and can’t be downloaded.',
      auth: 'Sign-in is required for this video.',
      format: 'The requested format isn’t available for this video.',
    };
    if (data && data.code && map[data.code]) return map[data.code];
    if (data && data.error) return data.error;
    return 'Something went wrong.';
  }

  function postJSON(url, body) {
    return fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {}),
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (!res.ok) throw new Error(friendlyError(data));
        return data;
      });
    });
  }

  function fetchJSON(url) {
    return fetch(url).then(function (res) {
      if (!res.ok) throw new Error('request failed');
      return res.json();
    });
  }

  // ---- boot --------------------------------------------------------

  function boot() {
    bindEvents();

    // An explicit ?url= (set when the CLI was invoked with -url) wins over
    // Securexe's current-context detection.
    var qUrl = new URLSearchParams(window.location.search).get('url');
    if (qUrl) {
      el('url').value = qUrl;
      loadUrl(qUrl);
      return;
    }

    fetchJSON('/api/context').then(function (ctx) {
      if (ctx && ctx.supported && ctx.url) {
        el('url').value = ctx.url;
        loadUrl(ctx.url);
      } else {
        el('url').focus();
      }
    }).catch(function () { el('url').focus(); });
  }

  function bindEvents() {
    el('load').addEventListener('click', function () { loadUrl(el('url').value.trim()); });
    el('url').addEventListener('keydown', function (e) {
      if (e.key === 'Enter') loadUrl(el('url').value.trim());
    });
    var maybeAutoLoad = debounce(function () {
      var v = el('url').value.trim();
      if (v && v !== state.url && looksLikeUrl(v)) loadUrl(v);
    }, 500);
    el('url').addEventListener('input', maybeAutoLoad);
    el('url').addEventListener('paste', function () { setTimeout(maybeAutoLoad, 0); });

    el('modeFull').addEventListener('click', function () { setMode('full'); });
    el('modeClip').addEventListener('click', function () { setMode('clip'); });

    el('startField').addEventListener('change', function () { onFieldChange('start'); });
    el('endField').addEventListener('change', function () { onFieldChange('end'); });

    el('save').addEventListener('click', onSave);
    el('cancelJob').addEventListener('click', onCancelJob);
    el('openFile').addEventListener('click', function () { if (state.lastPath) postJSON('/api/open', { path: state.lastPath }); });
    el('showFolder').addEventListener('click', function () { if (state.lastPath) postJSON('/api/reveal', { path: state.lastPath }); });
    el('copyPath').addEventListener('click', function () {
      if (state.lastPath && navigator.clipboard) navigator.clipboard.writeText(state.lastPath);
    });

    setupTimeline();
    setupPreview();
  }

  // ---- loading a URL -------------------------------------------------

  function loadUrl(url) {
    if (!url) return;
    state.url = url;
    hideError();
    closeJob();
    setLoading(true);
    postJSON('/api/inspect', { url: url }).then(function (info) {
      state.duration = info.duration || 0;
      state.start = 0;
      state.end = state.duration;
      renderMeta(info);
      renderQualities(info.qualities);
      renderPreview(info);
      showTimeline(info);
    }).catch(function (err) {
      showError(err.message);
      el('meta').classList.add('hidden');
      el('previewWrap').classList.add('hidden');
      el('timelineSection').classList.add('hidden');
    }).finally(function () {
      setLoading(false);
    });
  }

  function renderMeta(info) {
    el('meta').classList.remove('hidden');
    el('thumb').src = info.thumbnail || '';
    el('title').textContent = info.title || 'Untitled';
    var bits = [];
    if (info.uploader) bits.push(info.uploader);
    if (info.site) bits.push(info.site);
    if (info.duration) bits.push(formatLabel(info.duration));
    el('subtitle').textContent = bits.join(' · ');
  }

  function renderQualities(qualities) {
    var sel = el('quality');
    sel.innerHTML = '';
    var list = (qualities && qualities.length) ? qualities : ['best'];
    list.forEach(function (q) {
      var opt = document.createElement('option');
      opt.value = q;
      opt.textContent = q === 'best' ? 'Best' : q;
      sel.appendChild(opt);
    });
  }

  function renderPreview(info) {
    var video = el('preview');
    var fallback = el('previewFallback');
    el('previewWrap').classList.remove('hidden');
    fallback.classList.add('hidden');
    if (info.previewUrl) {
      video.classList.remove('hidden');
      video.src = info.previewUrl;
      video.load();
    } else {
      video.classList.add('hidden');
      video.removeAttribute('src');
      fallback.textContent = 'Preview unavailable for this video — timecodes still work.';
      fallback.classList.remove('hidden');
    }
  }

  function showTimeline(info) {
    var section = el('timelineSection');
    section.classList.remove('hidden');
    el('labelZero').textContent = '00:00';
    el('labelDuration').textContent = formatLabel(state.duration);

    var hasDuration = state.duration > 0 && !info.isLive;
    el('timeline').classList.toggle('disabled', !hasDuration);
    el('modeClip').disabled = !hasDuration;
    setMode(hasDuration ? state.mode : 'full');

    updateHandles();
    updateFields();
  }

  // ---- mode ----------------------------------------------------------

  function setMode(mode) {
    state.mode = mode;
    el('modeFull').classList.toggle('active', mode === 'full');
    el('modeClip').classList.toggle('active', mode === 'clip');
  }

  // ---- timeline --------------------------------------------------------

  function setupTimeline() {
    var timeline = el('timeline');
    var handleStart = el('handleStart');
    var handleEnd = el('handleEnd');

    timeline.addEventListener('pointerdown', function (e) {
      if (e.target === handleStart || e.target === handleEnd) return;
      seekPreview(timeFromEvent(e));
    });

    [handleStart, handleEnd].forEach(function (handle) {
      var which = handle.dataset.handle;
      handle.addEventListener('pointerdown', function (e) {
        e.stopPropagation();
        handle.setPointerCapture(e.pointerId);
        state.activeDragHandle = which;
      });
      handle.addEventListener('pointermove', function (e) {
        if (state.activeDragHandle !== which) return;
        setHandleTime(which, timeFromEvent(e));
      });
      handle.addEventListener('pointerup', function () { state.activeDragHandle = null; });
      handle.addEventListener('keydown', function (e) {
        var step = e.shiftKey ? 5 : 1;
        var current = which === 'start' ? state.start : state.end;
        if (e.key === 'ArrowLeft') { setHandleTime(which, current - step); e.preventDefault(); }
        if (e.key === 'ArrowRight') { setHandleTime(which, current + step); e.preventDefault(); }
      });
    });
  }

  function timeFromEvent(e) {
    var rect = el('timeline').getBoundingClientRect();
    var ratio = rect.width ? clamp((e.clientX - rect.left) / rect.width, 0, 1) : 0;
    return ratio * state.duration;
  }

  function setHandleTime(which, t) {
    t = clamp(t, 0, state.duration);
    var minGap = Math.min(0.5, state.duration || 0.5);
    if (which === 'start') {
      state.start = Math.min(t, state.end - minGap);
      state.start = Math.max(0, state.start);
    } else {
      state.end = Math.max(t, state.start + minGap);
      state.end = Math.min(state.duration, state.end);
    }
    updateHandles();
    updateFields();
    seekPreview(which === 'start' ? state.start : state.end);
    if (state.mode !== 'clip' && el('modeClip') && !el('modeClip').disabled) setMode('clip');
  }

  function updateHandles() {
    var d = state.duration || 1;
    var startPct = (state.start / d) * 100;
    var endPct = (state.end / d) * 100;
    el('handleStart').style.left = startPct + '%';
    el('handleEnd').style.left = endPct + '%';
    el('range').style.left = startPct + '%';
    el('range').style.width = Math.max(0, endPct - startPct) + '%';
  }

  function updateFields() {
    el('startField').value = formatField(state.start);
    el('endField').value = formatField(state.end);
    el('durationField').textContent = (state.end - state.start).toFixed(1);
  }

  function onFieldChange(which) {
    var field = el(which === 'start' ? 'startField' : 'endField');
    var t = parseTimecode(field.value);
    if (isNaN(t)) { updateFields(); return; }
    setHandleTime(which, t);
  }

  // ---- preview ---------------------------------------------------------

  function setupPreview() {
    var video = el('preview');
    video.addEventListener('timeupdate', function () {
      var d = state.duration || video.duration || 1;
      el('playhead').style.left = ((video.currentTime / d) * 100) + '%';
    });
    video.addEventListener('click', function () {
      if (video.paused) video.play(); else video.pause();
    });
    video.addEventListener('error', function () {
      video.classList.add('hidden');
      var fallback = el('previewFallback');
      fallback.textContent = 'Preview unavailable for this video — timecodes still work.';
      fallback.classList.remove('hidden');
    });
  }

  function seekPreview(t) {
    var video = el('preview');
    if (!video.classList.contains('hidden') && video.readyState > 0) {
      video.currentTime = clamp(t, 0, state.duration || video.duration || 0);
    }
    var d = state.duration || 1;
    el('playhead').style.left = ((t / d) * 100) + '%';
  }

  // ---- save / jobs -------------------------------------------------

  function onSave() {
    hideError();
    hideDone();
    var body = {
      url: state.url,
      mode: state.mode,
      format: el('format').value,
      quality: el('quality').value,
    };
    if (state.mode === 'clip') {
      body.start = state.start;
      body.end = state.end;
    }
    setSaving(true);
    postJSON('/api/jobs', body).then(function (res) {
      state.jobId = res.jobId;
      subscribeJob(res.jobId);
    }).catch(function (err) {
      setSaving(false);
      showError(err.message);
    });
  }

  // If the SSE connection drops before a terminal event arrives (server
  // restarted, network hiccup, EventSource stuck retrying against a dead
  // job) don't leave the UI frozen on "Resolving…" forever. The backend
  // heartbeats at least every 4s while a job runs, so this only needs to
  // outlast a couple of missed beats — it's not meant to catch "this is
  // taking a while" (clipping a long, high-quality source legitimately can).
  var STALL_TIMEOUT_MS = 20000;

  function subscribeJob(jobId) {
    showProgress();
    var es = new EventSource('/api/jobs/' + jobId + '/events');
    state.eventSource = es;

    var stallTimer = null;
    function resetStallTimer() {
      clearTimeout(stallTimer);
      stallTimer = setTimeout(function () {
        closeJob();
        setSaving(false);
        hideProgress();
        showError('Lost connection to Video Clipper while saving. Is it still running?');
      }, STALL_TIMEOUT_MS);
    }
    resetStallTimer();

    es.onmessage = function (msg) {
      resetStallTimer();
      var e = JSON.parse(msg.data);
      updateProgress(e);
      if (e.stage === 'done') {
        clearTimeout(stallTimer);
        closeJob();
        setSaving(false);
        hideProgress();
        showDone(e);
      } else if (e.stage === 'error') {
        clearTimeout(stallTimer);
        closeJob();
        setSaving(false);
        hideProgress();
        showError(friendlyError({ code: e.code, error: e.message }));
      } else if (e.stage === 'canceled') {
        clearTimeout(stallTimer);
        closeJob();
        setSaving(false);
        hideProgress();
      }
    };
  }

  function onCancelJob() {
    if (!state.jobId) return;
    postJSON('/api/jobs/' + state.jobId + '/cancel', {}).catch(function () {});
  }

  function closeJob() {
    if (state.eventSource) { state.eventSource.close(); state.eventSource = null; }
    state.jobId = null;
  }

  function updateProgress(e) {
    var pct = clamp(e.percent || 0, 0, 100);
    var fill = el('progressFill');
    var label;
    var indeterminate = e.stage === 'downloading' && !e.speed && !e.eta && pct === 0;

    fill.classList.toggle('indeterminate', indeterminate);
    if (indeterminate && e.message) {
      fill.style.width = '100%';
      label = e.message.charAt(0).toUpperCase() + e.message.slice(1);
    } else if (indeterminate) {
      fill.style.width = '100%';
      label = 'Downloading… (long or high-quality clips can take a while)';
    } else if (e.stage === 'downloading') {
      fill.style.width = pct + '%';
      label = 'Downloading ' + pct.toFixed(0) + '%';
      if (e.speed) label += ' · ' + e.speed;
      if (e.eta) label += ' · ETA ' + e.eta;
    } else if (e.stage === 'processing') {
      fill.style.width = '100%';
      label = 'Finishing up…';
    } else {
      fill.style.width = '0%';
      label = 'Resolving…';
    }
    el('progressLabel').textContent = label;
  }

  function showDone(e) {
    el('doneActions').classList.remove('hidden');
    el('doneMessage').textContent = e.filename || 'Saved.';
    state.lastPath = e.path || '';
  }

  // ---- small UI state helpers ----------------------------------------

  function setLoading(v) { el('loading').classList.toggle('hidden', !v); el('load').disabled = v; }
  function setSaving(v) { el('save').disabled = v; }
  function showError(msg) { el('error').textContent = msg; el('error').classList.remove('hidden'); }
  function hideError() { el('error').classList.add('hidden'); }
  function showProgress() { el('progress').classList.remove('hidden'); el('progressFill').style.width = '0%'; }
  function hideProgress() { el('progress').classList.add('hidden'); }
  function hideDone() { el('doneActions').classList.add('hidden'); }

  boot();
})();
