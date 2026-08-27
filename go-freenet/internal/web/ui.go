// Package web serves the FreeNet control panel — a single-page web app
// with a large toggle button, real-time log stream, stats, and strategy
// selector.
package web

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/logs"
	"github.com/mintfary-oss/freenet/internal/types"
)

// Controller is the interface the web UI uses to control the proxy server.
// proxy.Server satisfies this interface.
type Controller interface {
	Enabled() bool
	SetEnabled(bool)
	Strategy() string
	SetStrategy(string)
	GetStats() types.StatsSnapshot
	HostlistSize() int
	RunAutoDetect(target string) []types.ProbeResult
	// DNSEnabled reports whether the local DoH resolver is running.
	DNSEnabled() bool
	// DNSStats returns the total query and error counts from the DoH resolver.
	DNSStats() (queries, errors int64)
	// ECHPassthroughs returns the count of connections forwarded unmodified
	// because the ClientHello carried an ECH extension.
	ECHPassthroughs() int64
}

// UI is the HTTP server that hosts the web control panel.
type UI struct {
	addr       string
	cfg        *config.Config
	ctrl       Controller
	ring       *logs.Ring
	httpServer *http.Server
	upgrader   websocket.Upgrader
	// reportFn, when set, generates a plain-text diagnostic report.
	reportFn func() string
}

// SetReporter registers a function that produces the diagnostic report text.
// Call this before Start.
func (u *UI) SetReporter(fn func() string) {
	u.reportFn = fn
}

// NewUI constructs a UI. Call Start to begin listening.
func NewUI(addr string, cfg *config.Config, ctrl Controller, ring *logs.Ring) *UI {
	return &UI{
		addr: addr,
		cfg:  cfg,
		ctrl: ctrl,
		ring: ring,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
	}
}

// Start registers HTTP handlers and launches the server in a goroutine.
func (u *UI) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", u.handleIndex)
	mux.HandleFunc("/download", u.handleDownload)
	mux.HandleFunc("/api/status", u.handleStatus)
	mux.HandleFunc("/api/toggle", u.handleToggle)
	mux.HandleFunc("/api/strategy", u.handleStrategy)
	mux.HandleFunc("/api/stats", u.handleStats)
	mux.HandleFunc("/api/autodetect", u.handleAutoDetect)
	mux.HandleFunc("/api/report", u.handleReport)
	mux.HandleFunc("/ws/logs", u.handleLogsWS)

	u.httpServer = &http.Server{
		Addr:         u.addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // unlimited for WebSocket
	}

	go func() {
		if err := u.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("web ui error: %v", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the HTTP server.
func (u *UI) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = u.httpServer.Shutdown(ctx)
}

// ---- API types ----

type statusResponse struct {
	Enabled         bool   `json:"enabled"`
	Strategy        string `json:"strategy"`
	ListenAddr      string `json:"listen_addr"`
	HostlistSize    int    `json:"hostlist_size"`
	DNSEnabled      bool   `json:"dns_enabled"`
	DNSQueries      int64  `json:"dns_queries"`
	DNSErrors       int64  `json:"dns_errors"`
	ECHPassthroughs int64  `json:"ech_passthroughs"`
}

type toggleRequest struct {
	Enabled bool `json:"enabled"`
}

type strategyRequest struct {
	Strategy string `json:"strategy"`
}

type autoDetectRequest struct {
	Target string `json:"target"`
}

// ---- Handlers ----

func (u *UI) handleStatus(w http.ResponseWriter, _ *http.Request) {
	dnsQ, dnsE := u.ctrl.DNSStats()
	writeJSON(w, statusResponse{
		Enabled:         u.ctrl.Enabled(),
		Strategy:        u.ctrl.Strategy(),
		ListenAddr:      u.cfg.Proxy.ListenAddr,
		HostlistSize:    u.ctrl.HostlistSize(),
		DNSEnabled:      u.ctrl.DNSEnabled(),
		DNSQueries:      dnsQ,
		DNSErrors:       dnsE,
		ECHPassthroughs: u.ctrl.ECHPassthroughs(),
	})
}

func (u *UI) handleStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, u.ctrl.GetStats())
}

func (u *UI) handleToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req toggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u.ctrl.SetEnabled(req.Enabled)
	u.handleStatus(w, r)
}

func (u *UI) handleStrategy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req strategyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u.ctrl.SetStrategy(req.Strategy)
	u.handleStatus(w, r)
}

func (u *UI) handleAutoDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req autoDetectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	target := req.Target
	if target == "" {
		target = "youtube.com:443"
	}
	results := u.ctrl.RunAutoDetect(target)
	writeJSON(w, results)
}

// handleLogsWS streams log entries over a WebSocket connection.
func (u *UI) handleLogsWS(w http.ResponseWriter, r *http.Request) {
	conn, err := u.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	for _, e := range u.ring.Recent(100) {
		if err := conn.WriteJSON(e); err != nil {
			return
		}
	}

	ch := u.ring.Subscribe()
	defer u.ring.Unsubscribe(ch)

	for e := range ch {
		if err := conn.WriteJSON(e); err != nil {
			return
		}
	}
}

func (u *UI) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

// handleDownload redirects to the index page with the download tab active.
func (u *UI) handleDownload(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/?tab=download", http.StatusSeeOther)
}

// handleReport returns a plain-text diagnostic report.
// Pass ?download=1 to get a Content-Disposition: attachment header so the
// browser saves the file instead of displaying it inline.
func (u *UI) handleReport(w http.ResponseWriter, _ *http.Request) {
	if u.reportFn == nil {
		http.Error(w, "diagnostics not configured", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(u.reportFn()))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// indexHTML is the complete single-page control panel, inlined into the binary.
const indexHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>FreeNet — обход DPI</title>
<style>
:root{
  --bg:#0f0f13;--card:#1a1a22;--border:#2a2a38;
  --green:#22c55e;--red:#ef4444;--text:#e2e8f0;
  --muted:#64748b;--accent:#6366f1;--warn:#f59e0b;--blue:#3b82f6;
}
*{box-sizing:border-box;margin:0;padding:0}
body{
  background:var(--bg);color:var(--text);
  font-family:'Segoe UI',system-ui,sans-serif;
  min-height:100vh;display:flex;flex-direction:column;
  align-items:center;padding:1.5rem 1rem;gap:1.2rem;
}
h1{font-size:1.8rem;letter-spacing:.06em;color:var(--accent)}
.subtitle{font-size:.85rem;color:var(--muted);margin-top:-.6rem}

/* ── tab nav ─────────────────────────────────────────────────────── */
.tabs{display:flex;gap:.3rem;background:var(--card);border:1px solid var(--border);
  border-radius:.75rem;padding:.3rem;width:100%;max-width:520px;}
.tab{flex:1;padding:.55rem 1rem;border:none;background:none;color:var(--muted);
  font-size:.9rem;font-weight:600;border-radius:.5rem;cursor:pointer;transition:.15s;}
.tab.active{background:var(--accent);color:#fff;}
.tab:hover:not(.active){background:var(--border);color:var(--text);}

/* ── card ────────────────────────────────────────────────────────── */
.card{
  background:var(--card);border:1px solid var(--border);
  border-radius:1rem;padding:1.75rem;width:100%;max-width:520px;
}

/* ── big toggle ──────────────────────────────────────────────────── */
#toggle-btn{
  display:block;width:180px;height:180px;border-radius:50%;
  border:4px solid var(--border);background:var(--card);
  color:var(--text);font-size:1rem;font-weight:700;
  letter-spacing:.08em;cursor:pointer;margin:0 auto 1.2rem;
  transition:background .2s,border-color .2s,box-shadow .2s;outline:none;
}
#toggle-btn.on {background:var(--green);border-color:var(--green);box-shadow:0 0 48px rgba(34,197,94,.45);color:#fff}
#toggle-btn.off{background:var(--red);  border-color:var(--red);  box-shadow:0 0 36px rgba(239,68,68,.35);color:#fff}
#toggle-btn.loading{opacity:.6;cursor:wait}
#status-text{text-align:center;font-size:.85rem;color:var(--muted);margin-bottom:1rem}

/* ── form rows ───────────────────────────────────────────────────── */
.row{display:flex;align-items:center;gap:.6rem;margin-top:.9rem}
label{font-size:.82rem;color:var(--muted);white-space:nowrap;min-width:80px}
select,input[type=text]{
  flex:1;background:var(--bg);border:1px solid var(--border);
  color:var(--text);border-radius:.5rem;padding:.38rem .7rem;font-size:.88rem;
}
button.small{
  background:var(--accent);border:none;color:#fff;
  border-radius:.5rem;padding:.38rem .9rem;font-size:.82rem;
  cursor:pointer;white-space:nowrap;
}
button.small:hover{opacity:.85}
button.small.warn{background:var(--warn)}

/* ── stats grid ──────────────────────────────────────────────────── */
.stats{display:grid;grid-template-columns:1fr 1fr 1fr;gap:.5rem;margin-top:.9rem;}
.stat{background:var(--bg);border:1px solid var(--border);border-radius:.6rem;padding:.5rem .7rem;text-align:center;}
.stat .val{font-size:1.1rem;font-weight:700;color:var(--accent)}
.stat .lbl{font-size:.7rem;color:var(--muted);margin-top:.15rem}

/* ── probe results ───────────────────────────────────────────────── */
#probe-results{margin-top:.9rem;display:none}
.probe{display:flex;justify-content:space-between;align-items:center;
  padding:.3rem 0;border-bottom:1px solid var(--border);font-size:.82rem;}
.probe:last-child{border-bottom:none}
.probe .name{font-weight:600}
.probe .ok{color:var(--green)}.probe .fail{color:var(--red)}

/* ── log console ─────────────────────────────────────────────────── */
#log-box{
  background:#08080d;border:1px solid var(--border);
  border-radius:.75rem;padding:.9rem;height:260px;overflow-y:auto;
  font-family:'Consolas','Fira Mono',monospace;font-size:.76rem;
  line-height:1.65;color:#94a3b8;width:100%;max-width:680px;
}
.ll{margin:0;word-break:break-all}
.ll .ts{color:var(--muted);margin-right:.45em}
.addr{font-size:.78rem;color:var(--muted);text-align:center;margin-top:.5rem}

/* ── download page ───────────────────────────────────────────────── */
.dl-page{width:100%;max-width:680px;display:flex;flex-direction:column;gap:1rem;}
.os-banner{
  background:var(--card);border:1px solid var(--accent);border-radius:.75rem;
  padding:.9rem 1.2rem;display:flex;align-items:center;gap:.7rem;font-size:.9rem;
}
.os-banner .badge{
  background:var(--accent);color:#fff;border-radius:.4rem;
  padding:.2rem .65rem;font-size:.78rem;font-weight:700;
}
.dl-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:1rem;}
.dl-card{
  background:var(--card);border:1px solid var(--border);border-radius:1rem;
  padding:1.4rem;display:flex;flex-direction:column;gap:.7rem;
}
.dl-card.highlight{border-color:var(--accent);}
.dl-header{display:flex;align-items:center;gap:.6rem;}
.dl-icon{font-size:1.6rem;}
.dl-title{font-size:1.05rem;font-weight:700;}
.dl-desc{font-size:.82rem;color:var(--muted);}
.dl-btn{
  display:block;text-align:center;padding:.65rem 1.2rem;
  background:var(--accent);color:#fff;border-radius:.6rem;
  text-decoration:none;font-weight:600;font-size:.9rem;
  transition:opacity .15s;
}
.dl-btn:hover{opacity:.85;}
.dl-btn.secondary{background:var(--border);color:var(--text);}
.dl-code{
  background:#08080d;border:1px solid var(--border);border-radius:.5rem;
  padding:.55rem .8rem;font-family:'Consolas','Fira Mono',monospace;
  font-size:.75rem;color:#94a3b8;display:flex;justify-content:space-between;
  align-items:center;gap:.5rem;cursor:pointer;
}
.dl-code:hover{border-color:var(--accent);}
.dl-code .copy-hint{font-size:.7rem;color:var(--muted);white-space:nowrap;}
.dl-hint{font-size:.78rem;color:var(--muted);}
.all-releases{
  text-align:center;padding:.8rem;border:1px solid var(--border);border-radius:.75rem;
  font-size:.85rem;color:var(--muted);
}
.all-releases a{color:var(--accent);text-decoration:none;}
.all-releases a:hover{text-decoration:underline;}
</style>
</head>
<body>

<h1>🌐 FreeNet</h1>
<p class="subtitle">Обход DPI — Россия / RKN / ТСПУ</p>

<!-- Tab navigation -->
<div class="tabs">
  <button class="tab" id="tab-ctrl-btn"   onclick="showTab('ctrl')"    >⚙️ Управление</button>
  <button class="tab" id="tab-dl-btn"     onclick="showTab('download')" >⬇️ Скачать</button>
  <button class="tab" id="tab-diag-btn"   onclick="showTab('diag')"     >📊 Диагностика</button>
</div>

<!-- ═══════════════════ TAB: Control Panel ═══════════════════ -->
<div id="tab-ctrl">
<div class="card">
  <button id="toggle-btn" onclick="toggle()">…</button>
  <div id="status-text">загрузка…</div>

  <div class="row">
    <label>Стратегия</label>
    <select id="strategy" onchange="setStrategy(this.value)">
      <option value="auto">auto (рекомендуется)</option>
      <option value="split">split — TCP фрагментация</option>
      <option value="tlsrec">tlsrec — TLS record split</option>
      <option value="combined">combined — максимум</option>
      <option value="disorder">disorder — перестановка</option>
      <option value="fake">fake — decoy пакет</option>
      <option value="none">none — без обхода</option>
    </select>
  </div>

  <div class="row">
    <label>Тест цели</label>
    <input type="text" id="probe-target" value="youtube.com:443" placeholder="host:port">
    <button class="small warn" onclick="runAutoDetect()">🔍 Авто</button>
  </div>

  <div id="probe-results"></div>

  <div class="stats">
    <div class="stat"><div class="val" id="s-active">0</div><div class="lbl">активных</div></div>
    <div class="stat"><div class="val" id="s-total">0</div><div class="lbl">всего</div></div>
    <div class="stat"><div class="val" id="s-bypassed">0</div><div class="lbl">обойдено</div></div>
  </div>

  <div class="addr" id="addr-text"></div>
  <div class="addr" id="dns-text" style="margin-top:.3rem"></div>
  <div class="addr" id="ech-text" style="margin-top:.3rem"></div>
</div>

<div id="log-box"></div>
</div><!-- /tab-ctrl -->

<!-- ═══════════════════ TAB: Download ═══════════════════ -->
<div id="tab-download" style="display:none">
<div class="dl-page">

  <!-- OS auto-detected banner -->
  <div class="os-banner" id="os-banner" style="display:none">
    <span id="os-banner-icon"></span>
    <span>Обнаружена: <span id="os-banner-name"></span></span>
    <span class="badge" id="os-banner-badge"></span>
    <span style="margin-left:auto;font-size:.8rem;color:var(--muted)">↓ рекомендуемый раздел выделен</span>
  </div>

  <div class="dl-grid">

    <!-- Android -->
    <div class="dl-card" id="card-android">
      <div class="dl-header"><span class="dl-icon">🤖</span><span class="dl-title">Android</span></div>
      <div class="dl-desc">APK — без root, VPN-режим перехватывает весь трафик</div>
      <a class="dl-btn" href="https://github.com/mintfary-oss/zapret2-may/releases/latest/download/freenet-android.apk"
         download>📥 Скачать APK</a>
      <div class="dl-hint">Настройки → Безопасность → Установка неизвестных приложений → Разрешить</div>
    </div>

    <!-- Windows -->
    <div class="dl-card" id="card-windows">
      <div class="dl-header"><span class="dl-icon">🪟</span><span class="dl-title">Windows</span></div>
      <div class="dl-desc">.exe устанавливается как служба, запускается автоматически при загрузке</div>
      <a class="dl-btn" href="https://github.com/mintfary-oss/zapret2-may/releases/latest/download/freenet-windows-amd64.exe"
         download>📥 Скачать .exe</a>
      <div class="dl-desc" style="margin-top:.2rem">Или PowerShell (Admin) — одна строка:</div>
      <div class="dl-code" onclick="copyText(this,'irm https://github.com/mintfary-oss/zapret2-may/releases/latest/download/install-windows.ps1 | iex')">
        <span>irm …/install-windows.ps1 | iex</span>
        <span class="copy-hint">📋 копировать</span>
      </div>
    </div>

    <!-- Linux -->
    <div class="dl-card" id="card-linux">
      <div class="dl-header"><span class="dl-icon">🐧</span><span class="dl-title">Linux</span></div>
      <div class="dl-desc">systemd сервис, amd64 / arm64 / ARMv7 (Raspberry Pi, роутеры)</div>
      <a class="dl-btn" href="https://github.com/mintfary-oss/zapret2-may/releases/latest/download/freenet-linux-amd64-installer.tar.gz"
         download>📥 Скачать installer.tar.gz</a>
      <div class="dl-desc" style="margin-top:.2rem">Или одна команда в терминале:</div>
      <div class="dl-code" onclick="copyText(this,'curl -fsSL https://github.com/mintfary-oss/zapret2-may/releases/latest/download/install.sh | sudo bash')">
        <span>curl …/install.sh | sudo bash</span>
        <span class="copy-hint">📋 копировать</span>
      </div>
      <div class="dl-hint">Для ARM64/ARMv7 скачайте нужный бинарник вручную на странице Releases.</div>
    </div>

    <!-- Docker -->
    <div class="dl-card" id="card-docker">
      <div class="dl-header"><span class="dl-icon">🐳</span><span class="dl-title">Docker</span></div>
      <div class="dl-desc">Один контейнер — Linux / NAS / VPS</div>
      <div class="dl-code" onclick="copyText(this,'git clone https://github.com/mintfary-oss/zapret2-may && cd zapret2-may/go-freenet && docker compose up -d')">
        <span>docker compose up -d</span>
        <span class="copy-hint">📋 копировать</span>
      </div>
      <div class="dl-hint">Веб-интерфейс → http://localhost:8080 · SOCKS5 → :1080</div>
    </div>

  </div>

  <div class="all-releases">
    Все версии и файлы:
    <a href="https://github.com/mintfary-oss/zapret2-may/releases" target="_blank">
      github.com/mintfary-oss/zapret2-may/releases →
    </a>
  </div>

</div><!-- /dl-page -->
</div><!-- /tab-download -->

<!-- ═══════════════════ TAB: Diagnostics ═══════════════════ -->
<div id="tab-diag" style="display:none">
<div style="width:100%;max-width:680px;display:flex;flex-direction:column;gap:.8rem;">
  <div style="display:flex;gap:.5rem;align-items:center;flex-wrap:wrap;">
    <span style="font-size:.85rem;color:var(--muted)">Состояние, статистика соединений, DNS/ECH, ошибки, журнал</span>
    <div style="margin-left:auto;display:flex;gap:.4rem;">
      <button class="small" onclick="loadReport()">🔄 Обновить</button>
      <button class="small" onclick="copyReport(this)">📋 Скопировать</button>
      <button class="small" onclick="downloadReport()">⬇️ Скачать .txt</button>
    </div>
  </div>
  <pre id="report-pre" style="background:#08080d;border:1px solid var(--border);border-radius:.75rem;padding:.9rem;height:440px;overflow-y:auto;font-family:'Consolas','Fira Mono',monospace;font-size:.74rem;line-height:1.6;color:#94a3b8;white-space:pre;word-break:normal;">загрузка…</pre>
</div>
</div><!-- /tab-diag -->

<script>
// ── Tab switching ─────────────────────────────────────────────────
function showTab(name){
  document.getElementById('tab-ctrl').style.display     = name==='ctrl'     ? '' : 'none';
  document.getElementById('tab-download').style.display = name==='download' ? '' : 'none';
  document.getElementById('tab-diag').style.display     = name==='diag'     ? '' : 'none';
  document.getElementById('log-box').style.display      = name==='ctrl'     ? '' : 'none';
  document.getElementById('tab-ctrl-btn').classList.toggle('active', name==='ctrl');
  document.getElementById('tab-dl-btn').classList.toggle('active',   name==='download');
  document.getElementById('tab-diag-btn').classList.toggle('active', name==='diag');
  // Persist tab in URL query.
  const u = new URL(location.href);
  u.searchParams.set('tab', name);
  history.replaceState({}, '', u);
  if(name==='diag') loadReport();
}

// Restore tab from URL on load.
(function(){
  const tab = new URLSearchParams(location.search).get('tab') || 'ctrl';
  showTab(tab);
})();

// ── OS auto-detection ─────────────────────────────────────────────
(function detectOS(){
  const ua = navigator.userAgent.toLowerCase();
  let os = null, icon = '', name = '', badge = '', cardId = '';
  if(/android/.test(ua)){
    os='android'; icon='🤖'; name='Android'; badge='Скачать APK'; cardId='card-android';
  } else if(/win/.test(ua)){
    os='windows'; icon='🪟'; name='Windows'; badge='Скачать .exe'; cardId='card-windows';
  } else if(/linux/.test(ua)){
    os='linux';   icon='🐧'; name='Linux';   badge='Скачать бинарник'; cardId='card-linux';
  } else if(/mac/.test(ua)){
    os='docker';  icon='🍎'; name='macOS (Docker)'; badge='Docker Compose'; cardId='card-docker';
  }
  if(!os) return;
  const banner = document.getElementById('os-banner');
  document.getElementById('os-banner-icon').textContent = icon;
  document.getElementById('os-banner-name').textContent = name;
  document.getElementById('os-banner-badge').textContent = badge;
  banner.style.display = 'flex';
  const card = document.getElementById(cardId);
  if(card) card.classList.add('highlight');
})();

// ── Copy to clipboard ─────────────────────────────────────────────
function copyText(el, text){
  navigator.clipboard.writeText(text).then(()=>{
    const hint = el.querySelector('.copy-hint');
    const orig = hint.textContent;
    hint.textContent = '✓ скопировано';
    setTimeout(()=>{ hint.textContent = orig; }, 1500);
  }).catch(()=>{});
}

// ── Diagnostics tab ─────────────────────────────────────────────────
async function loadReport(){
  const pre = document.getElementById('report-pre');
  if(!pre) return;
  pre.textContent = 'Загрузка…';
  try{
    const r = await fetch('/api/report');
    if(!r.ok){ pre.textContent = 'Ошибка ' + r.status; return; }
    pre.textContent = await r.text();
  } catch(e){
    pre.textContent = 'Ошибка соединения: ' + e;
  }
}

function copyReport(btn){
  const pre = document.getElementById('report-pre');
  navigator.clipboard.writeText(pre.textContent).then(()=>{
    const orig = btn.textContent;
    btn.textContent = '✓ Скопировано';
    setTimeout(()=>{ btn.textContent = orig; }, 1500);
  }).catch(()=> alert('Не удалось скопировать'));
}

function downloadReport(){
  const text = document.getElementById('report-pre').textContent;
  const blob = new Blob([text], {type:'text/plain;charset=utf-8'});
  const url  = URL.createObjectURL(blob);
  const a    = document.createElement('a');
  const ts   = new Date().toISOString().replace(/[:.]/g,'-').slice(0,19);
  a.href     = url;
  a.download = 'freenet-report-' + ts + '.txt';
  a.click();
  URL.revokeObjectURL(url);
}

// ── Control panel ─────────────────────────────────────────────────
let enabled = false;

async function loadStatus(){
  const r = await fetch('/api/status');
  const s = await r.json();
  enabled = s.enabled;
  updateBtn();
  document.getElementById('strategy').value = s.strategy;
  const hl = s.hostlist_size > 0 ? ' · домены: ' + s.hostlist_size : '';
  document.getElementById('addr-text').textContent = 'SOCKS5: ' + s.listen_addr + hl;
  const dnsEl = document.getElementById('dns-text');
  if(s.dns_enabled){
    dnsEl.innerHTML = '🔒 DNS-over-HTTPS активен · запросов: <b>' + s.dns_queries + '</b>' +
      (s.dns_errors > 0 ? ' · ошибок: <span style="color:var(--red)">' + s.dns_errors + '</span>' : '');
    dnsEl.style.color = 'var(--green)';
  } else {
    dnsEl.textContent = '⚠ DNS-over-HTTPS выключен (DNS может быть подменён)';
    dnsEl.style.color = 'var(--warn)';
  }
  const echEl = document.getElementById('ech-text');
  if(s.ech_passthroughs > 0){
    echEl.textContent = '🔐 ECH обнаружен: ' + s.ech_passthroughs + ' соед. (браузер уже шифрует SNI)';
    echEl.style.color = 'var(--green)';
  } else {
    echEl.textContent = '🔐 ECH: ожидание соединений с ECH-клиентами (Chrome/Firefox)';
    echEl.style.color = 'var(--muted)';
  }
}

function updateBtn(){
  const btn = document.getElementById('toggle-btn');
  const txt = document.getElementById('status-text');
  btn.classList.remove('on','off','loading');
  if(enabled){
    btn.className='on'; btn.textContent='ВКЛЮЧЕНО';
    txt.textContent='Обход DPI активен ✓';
  } else {
    btn.className='off'; btn.textContent='ВЫКЛЮЧЕНО';
    txt.textContent='Нажмите кнопку для включения';
  }
}

async function toggle(){
  document.getElementById('toggle-btn').classList.add('loading');
  enabled = !enabled;
  await fetch('/api/toggle',{
    method:'POST', headers:{'Content-Type':'application/json'},
    body: JSON.stringify({enabled})
  });
  updateBtn();
}

async function setStrategy(v){
  await fetch('/api/strategy',{
    method:'POST', headers:{'Content-Type':'application/json'},
    body: JSON.stringify({strategy:v})
  });
}

async function pollStats(){
  try{
    const r = await fetch('/api/stats');
    const s = await r.json();
    document.getElementById('s-active').textContent   = s.active;
    document.getElementById('s-total').textContent    = s.total;
    document.getElementById('s-bypassed').textContent = s.bypassed;
  } catch(_){}
}
setInterval(pollStats, 2000);

async function runAutoDetect(){
  const target = document.getElementById('probe-target').value || 'youtube.com:443';
  const box = document.getElementById('probe-results');
  box.style.display='block';
  box.innerHTML='<div style="color:var(--muted);font-size:.82rem">Тестирование стратегий…</div>';
  const r = await fetch('/api/autodetect',{
    method:'POST', headers:{'Content-Type':'application/json'},
    body: JSON.stringify({target})
  });
  const results = await r.json();
  box.innerHTML = results.map(p=>{
    const st = p.ok
      ? '<span class="ok">✓ '+Math.round(p.latency_ms/1e6)+'ms</span>'
      : '<span class="fail">✗ '+escHtml(p.err||'timeout')+'</span>';
    return '<div class="probe"><span class="name">'+escHtml(p.strategy)+'</span>'+st+'</div>';
  }).join('');
}

function connectLogs(){
  const proto = location.protocol==='https:'?'wss':'ws';
  const ws = new WebSocket(proto+'://'+location.host+'/ws/logs');
  const box = document.getElementById('log-box');
  ws.onmessage = e=>{
    const d = JSON.parse(e.data);
    const line = document.createElement('p');
    line.className='ll';
    const ts = new Date(d.time).toLocaleTimeString();
    line.innerHTML='<span class="ts">'+ts+'</span>'+escHtml(d.msg);
    box.appendChild(line);
    while(box.children.length > 500) box.removeChild(box.firstChild);
    box.scrollTop = box.scrollHeight;
  };
  ws.onclose = ()=>setTimeout(connectLogs,2000);
}

function escHtml(s){
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

loadStatus();
connectLogs();
pollStats();
</script>
</body>
</html>`
