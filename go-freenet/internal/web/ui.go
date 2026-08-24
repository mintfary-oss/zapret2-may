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
}

// UI is the HTTP server that hosts the web control panel.
type UI struct {
	addr       string
	cfg        *config.Config
	ctrl       Controller
	ring       *logs.Ring
	httpServer *http.Server
	upgrader   websocket.Upgrader
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
	mux.HandleFunc("/api/status", u.handleStatus)
	mux.HandleFunc("/api/toggle", u.handleToggle)
	mux.HandleFunc("/api/strategy", u.handleStrategy)
	mux.HandleFunc("/api/stats", u.handleStats)
	mux.HandleFunc("/api/autodetect", u.handleAutoDetect)
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
	Enabled      bool   `json:"enabled"`
	Strategy     string `json:"strategy"`
	ListenAddr   string `json:"listen_addr"`
	HostlistSize int    `json:"hostlist_size"`
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
	writeJSON(w, statusResponse{
		Enabled:      u.ctrl.Enabled(),
		Strategy:     u.ctrl.Strategy(),
		ListenAddr:   u.cfg.Proxy.ListenAddr,
		HostlistSize: u.ctrl.HostlistSize(),
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
<title>FreeNet</title>
<style>
:root{
  --bg:#0f0f13;--card:#1a1a22;--border:#2a2a38;
  --green:#22c55e;--red:#ef4444;--text:#e2e8f0;
  --muted:#64748b;--accent:#6366f1;--warn:#f59e0b;
}
*{box-sizing:border-box;margin:0;padding:0}
body{
  background:var(--bg);color:var(--text);
  font-family:'Segoe UI',system-ui,sans-serif;
  min-height:100vh;display:flex;flex-direction:column;
  align-items:center;padding:2rem 1rem;gap:1.5rem;
}
h1{font-size:1.8rem;letter-spacing:.06em;color:var(--accent)}
.subtitle{font-size:.85rem;color:var(--muted);margin-top:-.8rem}

/* ---- card ---- */
.card{
  background:var(--card);border:1px solid var(--border);
  border-radius:1rem;padding:1.75rem;width:100%;max-width:460px;
}

/* ---- big toggle ---- */
#toggle-btn{
  display:block;width:180px;height:180px;border-radius:50%;
  border:4px solid var(--border);background:var(--card);
  color:var(--text);font-size:1rem;font-weight:700;
  letter-spacing:.08em;cursor:pointer;margin:0 auto 1.2rem;
  transition:background .2s,border-color .2s,box-shadow .2s;outline:none;
}
#toggle-btn.on{background:var(--green);border-color:var(--green);box-shadow:0 0 48px rgba(34,197,94,.45);color:#fff}
#toggle-btn.off{background:var(--red);border-color:var(--red);box-shadow:0 0 36px rgba(239,68,68,.35);color:#fff}
#toggle-btn.loading{opacity:.6;cursor:wait}
#status-text{text-align:center;font-size:.85rem;color:var(--muted);margin-bottom:1rem}

/* ---- form rows ---- */
.row{display:flex;align-items:center;gap:.6rem;margin-top:.9rem}
label{font-size:.82rem;color:var(--muted);white-space:nowrap;min-width:80px}
select,input[type=text]{
  flex:1;background:var(--bg);border:1px solid var(--border);
  color:var(--text);border-radius:.5rem;padding:.38rem .7rem;
  font-size:.88rem;
}
button.small{
  background:var(--accent);border:none;color:#fff;
  border-radius:.5rem;padding:.38rem .9rem;font-size:.82rem;
  cursor:pointer;white-space:nowrap;
}
button.small:hover{opacity:.85}
button.small.warn{background:var(--warn)}

/* ---- stats grid ---- */
.stats{
  display:grid;grid-template-columns:1fr 1fr 1fr;gap:.5rem;
  margin-top:.9rem;
}
.stat{
  background:var(--bg);border:1px solid var(--border);
  border-radius:.6rem;padding:.5rem .7rem;text-align:center;
}
.stat .val{font-size:1.1rem;font-weight:700;color:var(--accent)}
.stat .lbl{font-size:.7rem;color:var(--muted);margin-top:.15rem}

/* ---- probe results ---- */
#probe-results{margin-top:.9rem;display:none}
.probe{
  display:flex;justify-content:space-between;align-items:center;
  padding:.3rem 0;border-bottom:1px solid var(--border);font-size:.82rem;
}
.probe:last-child{border-bottom:none}
.probe .name{font-weight:600}
.probe .ok{color:var(--green)}
.probe .fail{color:var(--red)}

/* ---- log console ---- */
#log-box{
  background:#08080d;border:1px solid var(--border);
  border-radius:.75rem;padding:.9rem;height:280px;overflow-y:auto;
  font-family:'Consolas','Fira Mono',monospace;font-size:.76rem;
  line-height:1.65;color:#94a3b8;width:100%;max-width:680px;
}
.ll{margin:0;word-break:break-all}
.ll .ts{color:var(--muted);margin-right:.45em}

.addr{font-size:.78rem;color:var(--muted);text-align:center;margin-top:.5rem}
</style>
</head>
<body>

<h1>🌐 FreeNet</h1>
<p class="subtitle">Обход DPI — Россия / RKN / ТСПУ</p>

<div class="card">
  <button id="toggle-btn" onclick="toggle()">…</button>
  <div id="status-text">загрузка…</div>

  <div class="row">
    <label>Стратегия</label>
    <select id="strategy" onchange="setStrategy(this.value)">
      <option value="auto">auto (рекомендуется)</option>
      <option value="split">split — TCP фрагментация</option>
      <option value="disorder">disorder — перестановка сегментов</option>
      <option value="none">none — без обхода</option>
    </select>
  </div>

  <div class="row">
    <label>Тест цели</label>
    <input type="text" id="probe-target" value="youtube.com:443" placeholder="host:port">
    <button class="small warn" onclick="runAutoDetect()">🔍 Авто</button>
  </div>

  <div id="probe-results"></div>

  <!-- live stats -->
  <div class="stats">
    <div class="stat"><div class="val" id="s-active">0</div><div class="lbl">активных</div></div>
    <div class="stat"><div class="val" id="s-total">0</div><div class="lbl">всего</div></div>
    <div class="stat"><div class="val" id="s-bypassed">0</div><div class="lbl">обойдено</div></div>
  </div>

  <div class="addr" id="addr-text"></div>
</div>

<div id="log-box"></div>

<script>
let enabled = false;

// ---- status ----
async function loadStatus(){
  const r = await fetch('/api/status');
  const s = await r.json();
  enabled = s.enabled;
  updateBtn();
  document.getElementById('strategy').value = s.strategy;
  const hl = s.hostlist_size > 0 ? ' · домены: ' + s.hostlist_size : '';
  document.getElementById('addr-text').textContent = 'SOCKS5: ' + s.listen_addr + hl;
}

function updateBtn(){
  const btn = document.getElementById('toggle-btn');
  const txt = document.getElementById('status-text');
  btn.classList.remove('on','off','loading');
  if(enabled){
    btn.className='on';
    btn.textContent='ВКЛЮЧЕНО';
    txt.textContent='Обход DPI активен ✓';
  } else {
    btn.className='off';
    btn.textContent='ВЫКЛЮЧЕНО';
    txt.textContent='Нажмите кнопку для включения';
  }
}

async function toggle(){
  const btn = document.getElementById('toggle-btn');
  btn.classList.add('loading');
  enabled = !enabled;
  await fetch('/api/toggle',{
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body: JSON.stringify({enabled})
  });
  updateBtn();
}

async function setStrategy(v){
  await fetch('/api/strategy',{
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body: JSON.stringify({strategy:v})
  });
}

// ---- stats polling (every 2s) ----
async function pollStats(){
  try{
    const r = await fetch('/api/stats');
    const s = await r.json();
    document.getElementById('s-active').textContent = s.active;
    document.getElementById('s-total').textContent = s.total;
    document.getElementById('s-bypassed').textContent = s.bypassed;
  } catch(_){}
}
setInterval(pollStats, 2000);

// ---- auto-detect ----
async function runAutoDetect(){
  const target = document.getElementById('probe-target').value || 'youtube.com:443';
  const box = document.getElementById('probe-results');
  box.style.display='block';
  box.innerHTML='<div style="color:var(--muted);font-size:.82rem">Тестирование стратегий…</div>';

  const r = await fetch('/api/autodetect',{
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body: JSON.stringify({target})
  });
  const results = await r.json();

  box.innerHTML = results.map(p=>{
    const st = p.ok
      ? '<span class="ok">✓ ' + Math.round(p.latency_ms/1e6) + 'ms</span>'
      : '<span class="fail">✗ ' + escHtml(p.err||'timeout') + '</span>';
    return '<div class="probe"><span class="name">'+escHtml(p.strategy)+'</span>'+st+'</div>';
  }).join('');
}

// ---- WebSocket log stream ----
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
    // keep at most 500 lines
    while(box.children.length > 500) box.removeChild(box.firstChild);
    box.scrollTop = box.scrollHeight;
  };
  ws.onclose = ()=>setTimeout(connectLogs,2000);
}

function escHtml(s){
  return String(s)
    .replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

loadStatus();
connectLogs();
pollStats();
</script>
</body>
</html>`
